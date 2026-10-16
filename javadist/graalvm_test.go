package javadist_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/setup-java/internal/testing/fakejdk"
	"github.com/circleci-functions/setup-java/javadist"
	"github.com/circleci-functions/setup-java/javaversion"
)

func graalvm(v javaversion.Version, p javadist.Platform) fakejdk.Build {
	return fakejdk.Build{Distribution: "graalvm", Version: v, Platform: p}
}

func TestGraalVM_Resolve(t *testing.T) {
	h := host(t)
	srv := fakejdk.New(t,
		graalvm(ver(21, 5, 9), h),
		graalvm(ver(21, 12, 8), h),
		graalvm(ver(25, 4, 7), h),
		fakejdk.Build{Version: ver(25, 4, 7)},
	)
	tests := []struct {
		name     string
		spec     string
		floating bool
		url      string
		want     fakejdk.Build
	}{
		{name: "major", spec: "21", floating: true, url: "/graalvm/21/latest/", want: graalvm(ver(21, 12, 8), h)},
		{name: "full version", spec: "21.0.5", url: "/graalvm/21/archive/", want: graalvm(ver(21, 5, 9), h)},
		{name: "latest", spec: "latest", floating: true, url: "/graalvm/25/latest/", want: graalvm(ver(25, 4, 7), h)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := resolveFrom(t, "graalvm", srv, tc.spec, h, "jdk")
			assert.NilError(t, err)
			assert.Equal(t, r.Floating, tc.floating)
			assert.Assert(t, strings.HasPrefix(r.URL, srv.URL+tc.url), r.URL)
			assert.Equal(t, r.SHA256, fakejdk.Checksum(tc.want))
			if !tc.floating {
				assert.Equal(t, r.Version.String(), "21.0.5")
			}
		})
	}
}

func TestGraalVM_Resolve_Errors(t *testing.T) {
	h := host(t)
	srv := fakejdk.New(t,
		graalvm(ver(21, 12, 8), h),
		fakejdk.Build{Version: ver(27, 0, 35)},
	)
	musl := javadist.Platform{OS: "linux", Arch: "x64", Libc: "musl"}
	tests := []struct {
		name     string
		spec     string
		platform javadist.Platform
		want     string
	}{
		{name: "latest without a build of the newest Java", spec: "latest", want: "Oracle GraalVM has no build of Java 27, the newest Java release, yet"},
		{name: "range", spec: ">=21", want: "Oracle GraalVM takes a major version such as 21"},
		{name: "build number", spec: "21.0.12+8", want: "Oracle GraalVM takes a major version such as 21"},
		{name: "too old", spec: "11", want: "Oracle GraalVM starts at Java 17"},
		{name: "unpublished version", spec: "21.0.7", want: "no matching Java version"},
		{name: "musl", spec: "21", platform: musl, want: fmt.Sprintf("Oracle GraalVM publishes no jdk releases for %s", musl)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.platform
			if p == (javadist.Platform{}) {
				p = h
			}
			_, err := resolveFrom(t, "graalvm", srv, tc.spec, p, "jdk")
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestGraalVM_InstallFloating(t *testing.T) {
	h := host(t)
	srv := fakejdk.New(t, graalvm(ver(21, 12, 8), h))
	r, err := resolveFrom(t, "graalvm", srv, "21", h, "jdk")
	assert.NilError(t, err)

	dest := filepath.Join(t.TempDir(), "jdk")
	assert.NilError(t, javadist.Install(context.Background(), r, dest))
	v, err := javadist.ReleaseVersion(javadist.Home(dest))
	assert.NilError(t, err)
	assert.Equal(t, v.String(), "21.0.12+8", "the version comes from the JDK's release file")

	d, _ := javadist.New("graalvm", "")
	assert.DeepEqual(t, javadist.Env(d, "/opt/graalvm"), map[string]string{"GRAALVM_HOME": "/opt/graalvm"})
	temurin, _ := javadist.New("temurin", "")
	assert.Assert(t, javadist.Env(temurin, "/opt/jdk") == nil)
}
