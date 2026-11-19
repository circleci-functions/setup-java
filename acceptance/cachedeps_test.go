//go:build acceptance

package setupjava_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"github.com/circleci-functions/toolkit/testing/runner"
)

func TestAcceptance_CacheDependencies(t *testing.T) {
	for _, tool := range []struct{ name, file, label string }{
		{name: "maven", file: "pom.xml", label: "Maven dependencies"},
		{name: "gradle", file: "build.gradle", label: "Gradle dependencies"},
		{name: "lein", file: "project.clj", label: "Leiningen dependencies"},
	} {
		t.Run(tool.name, func(t *testing.T) {
			s := sdktest.NewSandbox(t, sdktest.WithLiveCache())
			dir := t.TempDir()
			assert.NilError(t, os.WriteFile(filepath.Join(dir, tool.file), []byte("# "+s.CacheKeyNonce), 0600))

			r := runner.New(t, binaryPath, runner.WithSandboxEnv(s.Env()), runner.WithCmdDir(dir))
			_, err := r.Run("cache-dependencies", "--build-tool", tool.name, "--cache-nonce", s.CacheKeyNonce)
			out := r.StdoutString() + r.StderrString()
			assert.NilError(t, err, "output: %s", out)
			assert.Assert(t, strings.Contains(out, "restored "+tool.label+" if cached under setup-java-v1-"+tool.name+"-"), "output: %s", out)
			assert.Assert(t, strings.Contains(out, "saves it there when the job succeeds"), "output: %s", out)
		})
	}
}

var cachedUnder = regexp.MustCompile(`cached under (\S+?),? `)

// cacheDeps runs cache-dependencies in dir, with s's environment and nonce,
// and returns the key the dependencies are cached under.
func cacheDeps(t *testing.T, s *sdktest.Sandbox, nonce, dir string, args ...string) (string, string, error) {
	t.Helper()
	r := runner.New(t, binaryPath, runner.WithSandboxEnv(s.Env()), runner.WithCmdDir(dir))
	_, err := r.Run(append([]string{"cache-dependencies", "--build-tool", "maven", "--cache-nonce", nonce}, args...)...)
	out := r.StdoutString() + r.StderrString()
	m := cachedUnder.FindStringSubmatch(out)
	if m == nil {
		return "", out, err
	}
	return m[1], out, err
}

func TestAcceptance_CacheDependencies_KeyFollowsHome(t *testing.T) {
	a := sdktest.NewSandbox(t, sdktest.WithLiveCache())
	b := sdktest.NewSandbox(t, sdktest.WithLiveCache())
	dir := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<!-- "+a.CacheKeyNonce+" -->"), 0600))

	keyA, out, err := cacheDeps(t, a, a.CacheKeyNonce, dir)
	assert.NilError(t, err, "output: %s", out)
	keyB, out, err := cacheDeps(t, b, a.CacheKeyNonce, dir)
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, keyA != "" && keyB != "", "keys %q and %q", keyA, keyB)
	assert.Assert(t, keyA != keyB, "a cache restores to the home it was saved from, so different homes need different keys: %s", keyA)
}

func TestAcceptance_CacheDependencies_DependencyPathForms(t *testing.T) {
	s := sdktest.NewSandbox(t, sdktest.WithLiveCache())
	dir := t.TempDir()
	assert.NilError(t, os.MkdirAll(filepath.Join(dir, "server"), 0750))
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "server", "pom.xml"), []byte("<!-- "+s.CacheKeyNonce+" -->"), 0600))

	want, out, err := cacheDeps(t, s, s.CacheKeyNonce, dir, "--cache-dependency-path", "server/pom.xml")
	assert.NilError(t, err, "output: %s", out)
	for _, p := range []string{
		"./server/pom.xml",
		filepath.Join(dir, "server", "pom.xml"),
		filepath.Join(dir, "**", "pom.xml"),
	} {
		t.Run(p, func(t *testing.T) {
			got, out, err := cacheDeps(t, s, s.CacheKeyNonce, dir, "--cache-dependency-path", p)
			assert.NilError(t, err, "output: %s", out)
			assert.Equal(t, got, want, "the same build file, however it is written")
		})
	}

	t.Run("outside the working directory", func(t *testing.T) {
		_, out, err := cacheDeps(t, s, s.CacheKeyNonce, filepath.Join(dir, "server"), "--cache-dependency-path", filepath.Join(t.TempDir(), "pom.xml"))
		assert.Assert(t, err != nil, "output: %s", out)
		assert.Assert(t, strings.Contains(out, "is not under the working directory"), "output: %s", out)
	})
}
