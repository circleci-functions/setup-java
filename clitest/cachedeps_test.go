package clitest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"github.com/circleci-functions/toolkit/testing/runner"
)

// runIn runs the binary in s's environment with dir as its working
// directory.
func runIn(t *testing.T, s *sdktest.Sandbox, dir string, args ...string) (string, error) {
	t.Helper()
	r := runner.New(t, binaryPath, runner.WithSandboxEnv(s.Env()), runner.WithCmdDir(dir))
	_, err := r.Run(args...)
	return r.StdoutString() + r.StderrString(), err
}

func project(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		assert.NilError(t, os.MkdirAll(filepath.Dir(path), 0750))
		assert.NilError(t, os.WriteFile(path, []byte(content), 0600))
	}
	return dir
}

// download stands in for the build tool fetching a dependency into home.
func download(t *testing.T, path string) {
	t.Helper()
	assert.NilError(t, os.MkdirAll(filepath.Dir(path), 0750))
	assert.NilError(t, os.WriteFile(path, []byte("jar"), 0600))
}

var buildTools = []struct {
	tool    string
	files   map[string]string
	depPath []string
	// wrapperPath is empty for a build tool with no wrapper.
	wrapperPath []string
}{
	{
		tool:        "maven",
		files:       map[string]string{"pom.xml": "<project/>", ".mvn/wrapper/maven-wrapper.properties": "distributionUrl=x"},
		depPath:     []string{".m2", "repository", "junit", "junit.jar"},
		wrapperPath: []string{".m2", "wrapper", "dists", "maven.zip"},
	},
	{
		tool:        "gradle",
		files:       map[string]string{"build.gradle": "plugins {}", "gradle/wrapper/gradle-wrapper.properties": "distributionUrl=x"},
		depPath:     []string{".gradle", "caches", "modules-2", "junit.jar"},
		wrapperPath: []string{".gradle", "wrapper", "dists", "gradle.zip"},
	},
	{
		tool:    "lein",
		files:   map[string]string{"project.clj": `(defproject app "0.1.0")`},
		depPath: []string{".m2", "repository", "junit", "junit.jar"},
	},
}

func TestCLI_CacheDependencies_RoundTrip(t *testing.T) {
	for _, bt := range buildTools {
		for _, via := range []string{"cache-dependencies", "install"} {
			t.Run(bt.tool+" via "+via, func(t *testing.T) {
				f := newFixture(t)
				dir := project(t, bt.files)
				args := []string{"cache-dependencies", "--build-tool", bt.tool}
				if via == "install" {
					args = []string{"--distribution", "temurin", "--java-version", "21", "--cache-dependencies", bt.tool,
						"--api-url", f.srv.URL, "--install-dir", f.s.InstallDir, "--bin-dir", f.binDir}
				}

				out, err := runIn(t, f.s, dir, args...)
				assert.NilError(t, err, "output: %s", out)
				saves := f.s.Cache.PendingCacheSaves()
				paths := []string{filepath.Join(append([]string{f.s.Home}, bt.depPath...)...)}
				if bt.wrapperPath != nil {
					paths = append(paths, filepath.Join(append([]string{f.s.Home}, bt.wrapperPath...)...))
				}
				assert.Equal(t, len(saves), len(paths), "the dependencies, and the wrapper if any: %v", saves)
				assert.Assert(t, strings.HasPrefix(saves[0], "setup-java-v1-"+bt.tool+"-"), "%v", saves)

				for _, path := range paths {
					download(t, path)
				}
				f.s.Cache.EndJob(t)
				assert.NilError(t, os.RemoveAll(filepath.Join(f.s.Home, bt.depPath[0])))

				out, err = runIn(t, f.s, dir, args...)
				assert.NilError(t, err, "output: %s", out)
				for _, path := range paths {
					_, err = os.Stat(path)
					assert.NilError(t, err, "the next job restores what this one downloaded")
				}
			})
		}
	}
}

func TestCLI_CacheDependencies_NearMiss(t *testing.T) {
	s := sdktest.NewSandbox(t)
	dir := project(t, map[string]string{"pom.xml": "<project/>"})

	out, err := runIn(t, s, dir, "cache-dependencies", "--build-tool", "maven")
	assert.NilError(t, err, "output: %s", out)
	jar := filepath.Join(s.Home, ".m2", "repository", "junit.jar")
	download(t, jar)
	s.Cache.EndJob(t)
	assert.NilError(t, os.RemoveAll(filepath.Join(s.Home, ".m2")))

	assert.NilError(t, os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project><dependencies/></project>"), 0600))
	out, err = runIn(t, s, dir, "cache-dependencies", "--build-tool", "maven")
	assert.NilError(t, err, "output: %s", out)
	_, err = os.Stat(jar)
	assert.NilError(t, err, "a changed pom.xml still starts from the last cache")
	assert.Equal(t, len(s.Cache.PendingCacheSaves()), 1, "and saves under its new key")
}

func TestCLI_CacheDependencies_NotSaved(t *testing.T) {
	tests := []struct {
		name string
		args []string
		fail bool
	}{
		{name: "read-only", args: []string{"--cache-read-only"}},
		{name: "the job failed", fail: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := sdktest.NewSandbox(t)
			dir := project(t, map[string]string{"pom.xml": "<project/>"})
			args := append([]string{"cache-dependencies", "--build-tool", "maven"}, tc.args...)

			out, err := runIn(t, s, dir, args...)
			assert.NilError(t, err, "output: %s", out)
			download(t, filepath.Join(s.Home, ".m2", "repository", "junit.jar"))
			if tc.fail {
				s.Cache.EndJobAfterFailure(t)
			} else {
				assert.Equal(t, len(s.Cache.PendingCacheSaves()), 0)
				s.Cache.EndJob(t)
			}
			assert.Equal(t, len(s.Cache.SavedKeys()), 0)
		})
	}
}

func TestCLI_CacheDependencies_Rejects(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		args  []string
		want  string
	}{
		{name: "no build files", files: map[string]string{"README.md": ""}, args: []string{"cache-dependencies", "--build-tool", "maven"}, want: "no build files found for maven"},
		{name: "unknown build tool", args: []string{"cache-dependencies", "--build-tool", "sbt"}, want: `--build-tool must be one of "gradle","lein","maven"`},
		{name: "no build tool", args: []string{"cache-dependencies"}, want: "missing flags: --build-tool"},
		{name: "options without caching", args: []string{"--distribution", "temurin", "--java-version", "21", "--cache-read-only"}, want: "need --cache-dependencies"},
		{name: "install without build files", files: map[string]string{"README.md": ""}, args: []string{"--distribution", "temurin", "--java-version", "21", "--cache-dependencies", "gradle"}, want: "no build files found for gradle"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := sdktest.NewSandbox(t)
			out, err := runIn(t, s, project(t, tc.files), tc.args...)
			assert.Assert(t, err != nil, "output: %s", out)
			assert.Assert(t, strings.Contains(out, tc.want), "want %q in output: %s", tc.want, out)
		})
	}
}
