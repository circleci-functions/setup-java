package clitest_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"

	"github.com/circleci-functions/toolkit/sdk/sdktest"

	"github.com/circleci-functions/setup-java/internal/testing/fakejdk"
	"github.com/circleci-functions/setup-java/javadist"
	"github.com/circleci-functions/setup-java/javapath"
	"github.com/circleci-functions/setup-java/javaversion"
)

func ver(major, security, build int) javaversion.Version {
	return javaversion.Version{Major: major, Security: security, Build: build}
}

var builds = []fakejdk.Build{
	{Version: ver(17, 14, 7)},
	{Version: ver(21, 5, 11)},
	{Version: ver(21, 6, 7)},
	{Version: ver(21, 6, 7), Package: "jre"},
	{Version: ver(25, 1, 8)},
	{Version: javaversion.Version{Major: 28, Build: 19, EA: true}},
}

// fixture is a fake Adoptium API and the sandbox to install from it into.
type fixture struct {
	srv    *fakejdk.Server
	s      *sdktest.Sandbox
	run    runFunc
	binDir string
}

// newFixture needs a Unix: the fake JDKs' tools are shell scripts.
func newFixture(t *testing.T) fixture {
	t.Helper()
	skip.If(t, runtime.GOOS == "windows", "the fake JDKs' tools are shell scripts")
	s, run := sandbox(t)
	return fixture{srv: fakejdk.New(t, builds...), s: s, run: run, binDir: t.TempDir()}
}

// install runs the binary with the flags that point it at the fixture.
func (f fixture) install(t *testing.T, args ...string) (string, error) {
	t.Helper()
	args = append(args, "--api-url", f.srv.URL, "--install-dir", f.s.InstallDir, "--bin-dir", f.binDir)
	return f.run(t, nil, args...)
}

// java returns the version java in the fixture's bin dir reports.
func (f fixture) java(t *testing.T) string {
	t.Helper()
	_, version, err := javapath.Probe(filepath.Join(f.binDir, "java"))
	assert.NilError(t, err)
	return version
}

// exported returns what $BASH_ENV sets name to, or "".
func (f fixture) exported(t *testing.T, name string) string {
	t.Helper()
	if _, err := os.Stat(f.s.BashEnv); os.IsNotExist(err) {
		return ""
	}
	out, err := exec.Command("sh", "-c", `. "$1"; printenv "$2" || true`, "sh", f.s.BashEnv, name).Output()
	assert.NilError(t, err)
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	assert.NilError(t, os.WriteFile(path, []byte(content), 0600))
	return path
}

func TestCLI_Install_ResolvesVersion(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "major", args: []string{"--java-version", "21"}, want: "21.0.6"},
		{name: "full version", args: []string{"--java-version", "21.0.5"}, want: "21.0.5"},
		{name: "exact build", args: []string{"--java-version", "21.0.5+11"}, want: "21.0.5"},
		{name: "latest", args: []string{"--java-version", "latest"}, want: "25.0.1"},
		{name: "range", args: []string{"--java-version", ">=17 <25"}, want: "21.0.6"},
		{name: "early access", args: []string{"--java-version", "28-ea"}, want: "28-ea"},
		{name: "jre", args: []string{"--java-version", "21", "--java-package", "jre"}, want: "21.0.6"},
		{name: "version file", args: []string{"--java-version-file", writeFile(t, ".java-version", "17\n")}, want: "17.0.14"},
		{name: "sdkmanrc naming the distribution", args: []string{"--java-version-file", writeFile(t, ".sdkmanrc", "java=21.0.5-tem\n")}, want: "21.0.5"},
		{
			name: "version flag and file that agree",
			args: []string{"--java-version", "21", "--java-version-file", writeFile(t, ".tool-versions", "java temurin-21\n")},
			want: "21.0.6",
		},
		{name: "explicit install command", args: []string{"install", "--java-version", "17"}, want: "17.0.14"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			out, err := f.install(t, append(tc.args, "--distribution", "temurin")...)
			assert.NilError(t, err, "output: %s", out)
			assert.Equal(t, f.java(t), tc.want)
			assert.Assert(t, strings.HasPrefix(f.exported(t, "JAVA_HOME"), f.s.InstallDir), "output: %s", out)
		})
	}
}

func TestCLI_Install_SeveralVersions(t *testing.T) {
	f := newFixture(t)
	out, err := f.install(t, "--distribution", "temurin", "--java-version", "17,21")
	assert.NilError(t, err, "output: %s", out)

	assert.Equal(t, f.java(t), "21.0.6", "the last version is the default")
	host, err := javadist.Host("")
	assert.NilError(t, err)
	java17 := f.exported(t, javapath.HomeVar(17, host.Arch))
	java21 := f.exported(t, javapath.HomeVar(21, host.Arch))
	assert.Assert(t, strings.Contains(java17, "temurin-jdk-17.0.14_7-"), java17)
	assert.Equal(t, f.exported(t, "JAVA_HOME"), java21)
}

func TestCLI_Install_NotTheDefault(t *testing.T) {
	f := newFixture(t)
	out, err := f.install(t, "--distribution", "temurin", "--java-version", "21", "--make-default-jdk=false")
	assert.NilError(t, err, "output: %s", out)

	entries, err := os.ReadDir(f.binDir)
	assert.NilError(t, err)
	assert.Equal(t, len(entries), 0, "nothing is linked onto PATH")
	assert.Equal(t, f.exported(t, "JAVA_HOME"), "")
	host, err := javadist.Host("")
	assert.NilError(t, err)
	assert.Assert(t, f.exported(t, javapath.HomeVar(21, host.Arch)) != "")
}

func TestCLI_Install_Cache(t *testing.T) {
	f := newFixture(t)
	args := []string{"--distribution", "temurin", "--java-version", "21"}

	out, err := f.install(t, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.NilError(t, os.RemoveAll(f.s.InstallDir))

	out, err = f.install(t, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, strings.Contains(out, "restored Java 21.0.6+7 from cache"), "output: %s", out)
	assert.Equal(t, f.srv.Downloads(), 1)
	assert.Equal(t, f.java(t), "21.0.6")

	assert.NilError(t, os.RemoveAll(f.s.InstallDir))
	out, err = f.install(t, append(args, "--cache=false")...)
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, !strings.Contains(out, "from cache"), "output: %s", out)
	assert.Equal(t, f.srv.Downloads(), 2)
}

func TestCLI_Install_ExactVersionAlreadyInstalled(t *testing.T) {
	f := newFixture(t)
	args := []string{"--distribution", "temurin", "--java-version", "21.0.5+11"}

	out, err := f.install(t, args...)
	assert.NilError(t, err, "output: %s", out)
	before := len(f.srv.Requests())

	out, err = f.install(t, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.Equal(t, len(f.srv.Requests()), before, "an exact version already installed needs no lookup")
}

func TestCLI_Install_ChecksumMismatch(t *testing.T) {
	f := newFixture(t)
	f.srv.Corrupt()
	out, err := f.install(t, "--distribution", "temurin", "--java-version", "21")
	assert.Assert(t, err != nil, "output: %s", out)
	assert.Assert(t, strings.Contains(out, "checksum mismatch"), "output: %s", out)
}

func TestCLI_Install_JdkFile(t *testing.T) {
	host, err := javadist.Host("")
	assert.NilError(t, err)
	archive := writeFile(t, "jdk.tar.gz", string(fakejdk.Archive(fakejdk.Build{
		Version: ver(21, 5, 11), Package: "jdk", Platform: host,
	})))

	t.Run("installs the archive", func(t *testing.T) {
		f := newFixture(t)
		out, err := f.install(t, "--distribution", "jdkfile", "--jdk-file", archive, "--java-version", "21")
		assert.NilError(t, err, "output: %s", out)
		assert.Equal(t, f.java(t), "21.0.5")
		assert.Equal(t, len(f.srv.Requests()), 0, "a local archive needs no lookup")
	})

	t.Run("checks the major version", func(t *testing.T) {
		f := newFixture(t)
		out, err := f.install(t, "--distribution", "jdkfile", "--jdk-file", archive, "--java-version", "17")
		assert.Assert(t, err != nil, "output: %s", out)
		assert.Assert(t, strings.Contains(out, "is Java 21.0.5, not Java 17"), "output: %s", out)
	})
}

func TestCLI_Install_Rejects(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "no distribution", args: []string{"--java-version", "21"}, want: "missing flags: --distribution"},
		{name: "unknown distribution", args: []string{"--distribution", "zulu", "--java-version", "21"}, want: `--distribution must be one of "temurin","jdkfile" but got "zulu"`},
		{name: "no version", args: []string{"--distribution", "temurin"}, want: "one of --java-version or --java-version-file is required"},
		{name: "bad version", args: []string{"--distribution", "temurin", "--java-version", "twenty-one"}, want: "is not a version, a range or latest"},
		{
			name: "version flag and file that disagree",
			args: []string{"--distribution", "temurin", "--java-version", "21", "--java-version-file", writeFile(t, ".java-version", "17\n")},
			want: "ask for different versions",
		},
		{
			name: "file naming another distribution",
			args: []string{"--distribution", "temurin", "--java-version-file", writeFile(t, ".sdkmanrc", "java=21.0.5-zulu\n")},
			want: "asks for the zulu distribution (zulu), but --distribution is temurin",
		},
		{name: "missing version file", args: []string{"--distribution", "temurin", "--java-version-file", filepath.Join(t.TempDir(), ".java-version")}, want: "--java-version-file:"},
		{name: "unpublished package", args: []string{"--distribution", "temurin", "--java-version", "21", "--java-package", "jdk+fx"}, want: `temurin does not publish a "jdk+fx" package`},
		{name: "unknown architecture", args: []string{"--distribution", "temurin", "--java-version", "21", "--architecture", "sparc"}, want: `unsupported architecture "sparc"`},
		{name: "jdk-file without jdkfile", args: []string{"--distribution", "temurin", "--java-version", "21", "--jdk-file", "jdk.tar.gz"}, want: "--jdk-file needs --distribution jdkfile"},
		{name: "jdkfile without jdk-file", args: []string{"--distribution", "jdkfile", "--java-version", "21"}, want: "--distribution jdkfile needs --jdk-file"},
		{name: "jdkfile with a range", args: []string{"--distribution", "jdkfile", "--java-version", ">=21", "--jdk-file", "jdk.tar.gz"}, want: "needs one Java version"},
		{name: "jdkfile with several versions", args: []string{"--distribution", "jdkfile", "--java-version", "17,21", "--jdk-file", "jdk.tar.gz"}, want: "needs one Java version"},
		{name: "no such version", args: []string{"--distribution", "temurin", "--java-version", "22"}, want: "Temurin publishes no jdk releases"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			out, err := f.install(t, tc.args...)
			assert.Assert(t, err != nil, "expected a non-zero exit; output: %s", out)
			assert.Assert(t, strings.Contains(out, tc.want), "want %q in output: %s", tc.want, out)
			entries, _ := os.ReadDir(f.binDir)
			assert.Equal(t, len(entries), 0, "nothing is linked after a failure")
		})
	}
}
