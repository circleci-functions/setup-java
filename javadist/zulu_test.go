package javadist_test

import (
	"context"
	"fmt"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/setup-java/internal/testing/fakejdk"
	"github.com/circleci-functions/setup-java/javadist"
	"github.com/circleci-functions/setup-java/javaversion"
)

// resolveFrom resolves raw with the named distribution, looking builds up in
// srv.
func resolveFrom(t *testing.T, name string, srv *fakejdk.Server, raw string, p javadist.Platform, pkg string) (javadist.Release, error) {
	t.Helper()
	spec, err := javaversion.Parse(raw)
	assert.NilError(t, err)
	d, ok := javadist.New(name, srv.URL)
	assert.Assert(t, ok, name)
	return d.Resolve(context.Background(), spec, p, pkg)
}

func zulu(v javaversion.Version, pkg string, p javadist.Platform) fakejdk.Build {
	return fakejdk.Build{Distribution: "zulu", Version: v, Package: pkg, Platform: p}
}

func TestZulu_Resolve(t *testing.T) {
	h := javadist.Platform{}
	musl := javadist.Platform{OS: "linux", Arch: "x64", Libc: "musl"}
	arm := javadist.Platform{OS: "linux", Arch: "armv7", Libc: "glibc"}
	srv := fakejdk.New(t,
		zulu(ver(17, 14, 7), "", h),
		zulu(ver(21, 5, 11), "", h),
		zulu(ver(21, 6, 7), "", h),
		zulu(ver(25, 1, 8), "", h),
		zulu(ver(21, 6, 7), "jre", h),
		zulu(ver(21, 5, 11), "jdk+fx", h),
		zulu(ver(21, 7, 3), "jdk+crac", h),
		zulu(ver(21, 4, 7), "", musl),
		zulu(ver(17, 1, 12), "", arm),
		fakejdk.Build{Version: ver(21, 9, 1)},
	)
	tests := []struct {
		name     string
		spec     string
		platform javadist.Platform
		pkg      string
		want     string
	}{
		{name: "major", spec: "21", want: "21.0.6+7"},
		{name: "exact", spec: "21.0.5", want: "21.0.5+11"},
		{name: "latest", spec: "latest", want: "25.0.1+8"},
		{name: "range across majors", spec: ">=17 <25", want: "21.0.6+7"},
		{name: "jre", spec: "21", pkg: "jre", want: "21.0.6+7"},
		{name: "javafx", spec: "21", pkg: "jdk+fx", want: "21.0.5+11"},
		{name: "crac", spec: "21", pkg: "jdk+crac", want: "21.0.7+3"},
		{name: "musl", spec: "21", platform: musl, want: "21.0.4+7"},
		{name: "armv7", spec: "17", platform: arm, want: "17.0.1+12"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.platform
			if p == (javadist.Platform{}) {
				p = host(t)
			}
			pkg := tc.pkg
			if pkg == "" {
				pkg = "jdk"
			}
			r, err := resolveFrom(t, "zulu", srv, tc.spec, p, pkg)
			assert.NilError(t, err)
			assert.Equal(t, r.Version.String(), tc.want)
			want := zulu(r.Version, pkg, p)
			assert.Equal(t, r.SHA256, fakejdk.Checksum(want), "the checksum is looked up for the build picked")
		})
	}
}

func TestZulu_Resolve_Errors(t *testing.T) {
	srv := fakejdk.New(t, zulu(ver(21, 6, 7), "jdk", javadist.Platform{}))
	tests := []struct {
		name string
		spec string
		pkg  string
		want string
	}{
		{name: "no builds of that major", spec: "17", pkg: "jdk", want: fmt.Sprintf("Zulu publishes no jdk releases for %s", host(t))},
		{name: "no such version", spec: "21.0.5", pkg: "jdk", want: "no matching Java version"},
		{name: "no javafx build", spec: "21", pkg: "jdk+fx", want: "Zulu publishes no jdk+fx releases"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveFrom(t, "zulu", srv, tc.spec, host(t), tc.pkg)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}
