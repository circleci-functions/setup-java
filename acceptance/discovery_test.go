//go:build acceptance

package setupjava_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"

	"github.com/circleci-functions/toolkit/sdk"
	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"github.com/circleci-functions/toolkit/testing/runner"
)

// TestAcceptance_InstallsOntoTheRealPath is the one test that lets setup-java
// choose where java is linked, rather than telling it with --bin-dir. That
// choice depends on the executor, and a sandboxed suite never reaches it.
//
// It changes the machine it runs on, up to taking over a JDK already there,
// so it runs only where that is disposable: a CircleCI job, or a container
// the caller vouches for with $SETUP_JAVA_DISPOSABLE (see `task smoke-local`).
func TestAcceptance_InstallsOntoTheRealPath(t *testing.T) {
	if os.Getenv("SETUP_JAVA_DISPOSABLE") == "" {
		sdktest.SkipUnlessInJob(t)
	}

	s := sdktest.NewSandbox(t, sdktest.WithLiveCache())
	bashEnv := filepath.Join(t.TempDir(), "bash_env")
	run := func(t *testing.T) string {
		t.Helper()
		r := runner.New(t, binaryPath,
			runner.WithUnsafeOSEnvVars(),
			runner.WithUnsafeAdditionalEnvVars(map[string]string{"BASH_ENV": bashEnv}),
		)
		_, err := r.Run(
			"--distribution", "temurin",
			"--java-version", acceptanceVersion(),
			"--install-dir", s.InstallDir,
			"--cache-nonce", s.CacheKeyNonce,
		)
		out := r.StdoutString() + r.StderrString()
		assert.NilError(t, err, "got: %s", out)
		return out
	}

	out := run(t)
	t.Logf("setup-java: %s", out)
	assert.Assert(t, !strings.Contains(out, "from cache"),
		"a per-run nonce must miss the cache, so this install downloads for real; got: %s", out)

	version := reported(t, out, `Java (\S+) is the default`)
	linked := reported(t, out, `linked into (.+)$`)
	short, _, _ := strings.Cut(version, "+")
	t.Logf("setup-java installed Java %s and linked it into %s", version, linked)
	removeLinks(t, linked)

	t.Run("ResolvesByNameInALaterStep", func(t *testing.T) {
		got, err := sdktest.LaterStep(t, "java", "-version")
		assert.NilError(t, err, "a later step could not resolve java: %s", got)
		assert.Assert(t, strings.Contains(got, `"`+short+`"`), "a later step runs another java: %s", got)
		got, err = sdktest.LaterStep(t, "javac", "-version")
		assert.NilError(t, err, "a later step could not resolve javac: %s", got)
		assert.Assert(t, strings.Contains(got, "javac "+short), "a later step runs another javac: %s", got)
	})

	if runtime.GOOS == "windows" {
		t.Run("ResolvesInPowerShellToo", func(t *testing.T) {
			got, err := sdktest.LaterStep(t, "powershell.exe", "-Command", "java -version 2>&1 | Out-String")
			assert.NilError(t, err, "PowerShell could not resolve java: %s", got)
			assert.Assert(t, strings.Contains(got, `"`+short+`"`), "PowerShell resolved a different java: %s", got)
		})
	}

	t.Run("JavaHomeReachesALaterShellStep", func(t *testing.T) {
		got, err := sdktest.LaterStep(t, "sh", "-c", `. "$1" && "$JAVA_HOME/bin/java" -version`, "sh", bashEnv)
		assert.NilError(t, err, "JAVA_HOME does not lead to a JDK that runs: %s", got)
		assert.Assert(t, strings.Contains(got, `"`+short+`"`), "JAVA_HOME points at another JDK: %s", got)
	})

	t.Run("RestoresFromTheCacheOnAFreshExecutor", func(t *testing.T) {
		skip.If(t, !sdk.InCircleCIJob(), "the cache round trip needs a CircleCI job's cache")
		assert.NilError(t, os.RemoveAll(s.InstallDir))
		out := run(t)
		assert.Assert(t, strings.Contains(out, "restored Java "+version+" from cache"),
			"expected the install saved above to be restored; got: %s", out)
		got, err := sdktest.LaterStep(t, "java", "-version")
		assert.NilError(t, err, got)
		assert.Assert(t, strings.Contains(got, `"`+short+`"`), got)
	})

	t.Run("NotIntoADirectoryOwnedByAnotherTool", func(t *testing.T) {
		for _, part := range strings.Split(filepath.ToSlash(linked), "/") {
			assert.Assert(t, part != "shims",
				"a version manager rewrites its shims directory, taking the link with it: %s", linked)
		}
	})
}

// removeLinks takes the JDK tools back off PATH at the end of the test.
func removeLinks(t *testing.T, dir string) {
	t.Helper()
	t.Cleanup(func() {
		for _, name := range []string{"java", "javac", "jar", "jshell", "keytool", "javadoc", "jlink"} {
			for _, ext := range []string{"", ".exe", ".bat"} {
				_ = os.Remove(filepath.Join(dir, name+ext))
			}
		}
	})
}

// reported returns the first submatch of pattern in setup-java's output.
func reported(t *testing.T, out, pattern string) string {
	t.Helper()
	re := regexp.MustCompile(pattern)
	for line := range strings.SplitSeq(out, "\n") {
		if m := re.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			return m[1]
		}
	}
	t.Fatalf("setup-java did not report %q:\n%s", pattern, out)
	return ""
}
