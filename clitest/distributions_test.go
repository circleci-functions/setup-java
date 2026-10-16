package clitest_test

import (
	"os"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/setup-java/internal/testing/fakejdk"
	"github.com/circleci-functions/setup-java/javaversion"
)

func TestCLI_Install_Distributions(t *testing.T) {
	tests := []struct {
		distribution string
		pkg          string
		build        javaversion.Version
		want         string
	}{
		{distribution: "zulu", build: ver(21, 6, 7), want: "21.0.6"},
		{distribution: "zulu", pkg: "jdk+fx", build: ver(21, 5, 11), want: "21.0.5"},
		{distribution: "corretto", build: javaversion.Version{Major: 21, Security: 12, Build: 12, Revision: 1}, want: "21.0.12"},
		{distribution: "microsoft", build: ver(21, 6, 0), want: "21.0.6"},
		{distribution: "oracle-openjdk", build: ver(21, 2, 13), want: "21.0.2"},
		{distribution: "graalvm", build: ver(21, 12, 8), want: "21.0.12"},
	}
	for _, tc := range tests {
		pkg := tc.pkg
		if pkg == "" {
			pkg = "jdk"
		}
		t.Run(tc.distribution+" "+pkg, func(t *testing.T) {
			f := newFixtureWith(t, fakejdk.Build{Distribution: tc.distribution, Version: tc.build, Package: pkg})
			out, err := f.install(t, "--distribution", tc.distribution, "--java-version", "21", "--java-package", pkg)
			assert.NilError(t, err, "output: %s", out)
			assert.Equal(t, f.java(t), tc.want)
			assert.Assert(t, strings.Contains(out, "("+tc.distribution+" "+pkg+") installed at"), "output: %s", out)
		})
	}
}

func TestCLI_Install_GraalVM(t *testing.T) {
	older := fakejdk.Build{Distribution: "graalvm", Version: ver(21, 11, 9)}
	newer := fakejdk.Build{Distribution: "graalvm", Version: ver(21, 12, 8)}
	f := newFixtureWith(t, older)
	args := []string{"--distribution", "graalvm", "--java-version", "21"}

	out, err := f.install(t, args...)
	assert.NilError(t, err, "output: %s", out)
	assert.Assert(t, strings.Contains(out, `resolved "21" to the newest graalvm build of Java 21`), "output: %s", out)
	assert.Assert(t, strings.Contains(out, "Java 21.0.11+9 (graalvm jdk) installed at"), "the version is read from the JDK; output: %s", out)
	assert.Equal(t, f.exported(t, "GRAALVM_HOME"), f.exported(t, "JAVA_HOME"))

	t.Run("restores the same build from the cache", func(t *testing.T) {
		assert.NilError(t, os.RemoveAll(f.s.InstallDir))
		out, err := f.install(t, args...)
		assert.NilError(t, err, "output: %s", out)
		assert.Assert(t, strings.Contains(out, "restored Java 21 from cache"), "output: %s", out)
		assert.Equal(t, f.srv.Downloads(), 1)
		assert.Equal(t, f.java(t), "21.0.11")
	})

	t.Run("downloads a new build Oracle put at the same link", func(t *testing.T) {
		f.srv = fakejdk.New(t, newer)
		out, err := f.install(t, args...)
		assert.NilError(t, err, "output: %s", out)
		assert.Assert(t, !strings.Contains(out, "from cache"), "output: %s", out)
		assert.Equal(t, f.srv.Downloads(), 1)
		assert.Equal(t, f.java(t), "21.0.12")
	})
}

func TestCLI_Install_RejectsEarlyAccess(t *testing.T) {
	for _, distribution := range []string{"zulu", "corretto", "microsoft", "oracle-openjdk", "graalvm"} {
		t.Run(distribution, func(t *testing.T) {
			f := newFixtureWith(t)
			out, err := f.install(t, "--distribution", distribution, "--java-version", "28-ea")
			assert.Assert(t, err != nil, "output: %s", out)
			assert.Assert(t, strings.Contains(out, "setup-java installs no early-access builds of "+distribution), "output: %s", out)
			assert.Equal(t, len(f.srv.Requests()), 0, "rejected before anything is looked up")
		})
	}
}
