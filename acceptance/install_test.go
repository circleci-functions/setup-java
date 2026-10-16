//go:build acceptance

package setupjava_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"

	"github.com/circleci-functions/toolkit/pathlink"

	"github.com/circleci-functions/setup-java/javadist"
)

func TestAcceptance_Install(t *testing.T) {
	tests := []struct {
		distribution string
		args         []string
		version      *regexp.Regexp
		// noBuild says why the distribution publishes no build for this
		// machine, if so.
		noBuild string
	}{
		{distribution: "temurin", args: []string{"--java-version", acceptanceVersion()}, version: regexp.MustCompile(`^` + regexp.QuoteMeta(acceptanceVersion()) + `\b`)},
		{distribution: "temurin", args: []string{"--java-version", "21", "--java-package", "jre"}, version: regexp.MustCompile(`^21\.`)},
		{distribution: "temurin", args: []string{"--java-version", "8"}, version: regexp.MustCompile(`^1\.8\.0_`), noBuild: java8Missing()},
		{distribution: "zulu", args: []string{"--java-version", "21"}, version: regexp.MustCompile(`^21\.`)},
		{distribution: "zulu", args: []string{"--java-version", "17", "--java-package", "jdk+fx"}, version: regexp.MustCompile(`^17\.`)},
		{distribution: "corretto", args: []string{"--java-version", "21"}, version: regexp.MustCompile(`^21\.`)},
		{distribution: "microsoft", args: []string{"--java-version", "21"}, version: regexp.MustCompile(`^21\.`)},
		{distribution: "oracle-openjdk", args: []string{"--java-version", "21"}, version: regexp.MustCompile(`^21\.0\.2$`)},
		{distribution: "graalvm", args: []string{"--java-version", "21"}, version: regexp.MustCompile(`^21\.`)},
		{distribution: "graalvm", args: []string{"--java-version", "21.0.5"}, version: regexp.MustCompile(`^21\.0\.5$`)},
	}
	for _, tc := range tests {
		t.Run(tc.distribution+" "+strings.Join(tc.args, " "), func(t *testing.T) {
			skip.If(t, tc.noBuild != "", tc.noBuild)
			s, run := sandbox(t)
			args := append(tc.args, "--distribution", tc.distribution, "--install-dir", s.InstallDir, "--bin-dir", s.BinDir, "--cache=false")
			out, err := run(t, nil, args...)
			assert.NilError(t, err, "output: %s", out)

			got, err := linkedJava(s.BinDir).CombinedOutput()
			assert.NilError(t, err, "java -version: %s", got)
			version := javaVersion(t, string(got))
			assert.Assert(t, tc.version.MatchString(version), "java reports %s; want %s", version, tc.version)
		})
	}
}

func TestAcceptance_InstallTemurin_CacheRoundTrip(t *testing.T) {
	s, run := sandbox(t)
	args := []string{
		"--distribution", "temurin",
		"--java-version", acceptanceVersion(),
		"--install-dir", s.InstallDir,
		"--bin-dir", s.BinDir,
		"--cache-nonce", s.CacheKeyNonce,
	}

	out, err := run(t, nil, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, !strings.Contains(out, "from cache"), "a fresh nonce must miss the cache; got: %s", out)

	assert.NilError(t, os.RemoveAll(s.InstallDir))
	out, err = run(t, nil, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, strings.Contains(out, "from cache"), "expected the install saved above to be restored; got: %s", out)

	got, err := linkedJava(s.BinDir).CombinedOutput()
	assert.NilError(t, err, "the restored JDK does not run: %s", got)
}

func java8Missing() string {
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		return "Temurin publishes no Java 8 for Apple silicon"
	}
	return ""
}

// noBuild says why distribution publishes no build for this machine, if so.
func noBuild(t *testing.T, distribution string) string {
	t.Helper()
	if musl(t) && (distribution == "oracle-openjdk" || distribution == "graalvm") {
		return distribution + " publishes no musl builds"
	}
	return ""
}

// otherVersion is the Java version to install of distribution on this
// machine: 21, or 17 for Microsoft on musl, which has only 11 and 17.
func otherVersion(t *testing.T, distribution string) string {
	t.Helper()
	if musl(t) && distribution == "microsoft" {
		return "17"
	}
	return "21"
}

func musl(t *testing.T) bool {
	t.Helper()
	p, err := javadist.Host("")
	assert.NilError(t, err)
	return p.Libc == "musl"
}

// linkedJava returns a command running java -version through its link in
// binDir, a .bat shim on Windows.
func linkedJava(binDir string) *exec.Cmd {
	name := "java"
	if runtime.GOOS == "windows" {
		name = "java.bat"
	}
	return pathlink.Command(filepath.Join(binDir, name), "-version")
}

var javaVersionLine = regexp.MustCompile(`version "([^"]+)"`)

// javaVersion returns the version in java -version's output.
func javaVersion(t *testing.T, out string) string {
	t.Helper()
	m := javaVersionLine.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no version in java -version output:\n%s", out)
	}
	return m[1]
}
