package javapath_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"

	"github.com/circleci-functions/setup-java/internal/testing/fakejdk"
	"github.com/circleci-functions/setup-java/javapath"
	"github.com/circleci-functions/setup-java/javaversion"
)

// fakeJDK writes a JDK whose bin/java is a script answering like Java 21,
// and returns its home.
func fakeJDK(t *testing.T) string {
	t.Helper()
	skip.If(t, runtime.GOOS == "windows", "the fake java is a shell script")
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	assert.NilError(t, os.MkdirAll(bin, 0750))
	java := fakejdk.JavaScript(javaversion.Version{Major: 21, Security: 5, Build: 11})
	//nolint:gosec // test scripts must be executable
	assert.NilError(t, os.WriteFile(filepath.Join(bin, "java"), []byte(java), 0700))
	for _, name := range []string{"javac", "jar"} {
		//nolint:gosec // test scripts must be executable
		assert.NilError(t, os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0700))
	}
	assert.NilError(t, os.WriteFile(filepath.Join(bin, "README"), []byte("not a tool"), 0600))
	return home
}

func TestTools(t *testing.T) {
	home := fakeJDK(t)
	names, err := javapath.Tools(filepath.Join(home, "bin"))
	assert.NilError(t, err)
	assert.DeepEqual(t, names, []string{"java", "jar", "javac"})
}

func TestProbe(t *testing.T) {
	home := fakeJDK(t)
	got, version, err := javapath.Probe(filepath.Join(home, "bin", "java"))
	assert.NilError(t, err)
	assert.Equal(t, got, realPath(t, home))
	assert.Equal(t, version, "21.0.5")
}

func TestProbe_DoesNotRun(t *testing.T) {
	home := fakeJDK(t)
	java := filepath.Join(home, "bin", "java")
	assert.NilError(t, os.WriteFile(java, []byte("#!/bin/sh\necho 'cannot execute binary file' >&2\nexit 126\n"), 0600))
	assert.NilError(t, os.Chmod(java, 0700)) //nolint:gosec // test scripts must be executable

	_, _, err := javapath.Probe(java)
	assert.ErrorContains(t, err, "does not run")
	assert.ErrorContains(t, err, "cannot execute binary file")
}

func TestLink(t *testing.T) {
	home := fakeJDK(t)
	binDir := t.TempDir()

	res, err := javapath.Link(javapath.Options{Home: home, BinDir: binDir})
	assert.NilError(t, err)
	assert.Equal(t, res, javapath.Result{Dir: binDir})
	for _, name := range []string{"java", "javac", "jar"} {
		target, err := os.Readlink(filepath.Join(binDir, name))
		assert.NilError(t, err, name)
		assert.Equal(t, target, filepath.Join(home, "bin", name))
	}
	got, _, err := javapath.Probe(filepath.Join(binDir, "java"))
	assert.NilError(t, err)
	assert.Equal(t, got, realPath(t, home), "java run through its link finds the JDK it belongs to")
}

func TestHomeVar(t *testing.T) {
	assert.Equal(t, javapath.HomeVar(21, "x64"), "JAVA_HOME_21_X64")
	assert.Equal(t, javapath.HomeVar(8, "aarch64"), "JAVA_HOME_8_AARCH64")
}

func TestExport(t *testing.T) {
	skip.If(t, runtime.GOOS == "windows", "checked with /bin/sh")
	bashEnv := filepath.Join(t.TempDir(), "bash_env")
	t.Setenv("BASH_ENV", bashEnv)

	home := "/opt/jdk's home/21"
	assert.NilError(t, javapath.Export(map[string]string{"JAVA_HOME": home}))

	out, err := exec.Command("sh", "-c", `. "$1"; printf %s "$JAVA_HOME"`, "sh", bashEnv).Output()
	assert.NilError(t, err)
	assert.Equal(t, string(out), home, "a later step reads the value back unchanged")
}

func TestPrependPath(t *testing.T) {
	skip.If(t, runtime.GOOS == "windows", "checked with /bin/sh")
	bashEnv := filepath.Join(t.TempDir(), "bash_env")
	t.Setenv("BASH_ENV", bashEnv)

	dir := "/opt/jdk's home/bin"
	assert.NilError(t, javapath.PrependPath(dir))

	out, err := exec.Command("sh", "-c", `PATH=/usr/bin:/bin; . "$1"; printf %s "$PATH"`, "sh", bashEnv).Output()
	assert.NilError(t, err)
	assert.Equal(t, string(out), dir+":/usr/bin:/bin")
}

func TestExport_NoBashEnv(t *testing.T) {
	t.Setenv("BASH_ENV", "")
	err := javapath.Export(map[string]string{"JAVA_HOME": "/opt/jdk"})
	assert.ErrorContains(t, err, "BASH_ENV is not set")
	assert.ErrorContains(t, javapath.PrependPath("/opt/jdk/bin"), "BASH_ENV is not set")
}

func realPath(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	assert.NilError(t, err)
	return strings.TrimRight(r, string(os.PathSeparator))
}
