package javadist_test

import (
	"fmt"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/setup-java/internal/testing/fakejdk"
	"github.com/circleci-functions/setup-java/javadist"
	"github.com/circleci-functions/setup-java/javaversion"
)

func microsoft(v javaversion.Version, p javadist.Platform) fakejdk.Build {
	return fakejdk.Build{Distribution: "microsoft", Version: v, Platform: p}
}

func TestMicrosoft_Resolve(t *testing.T) {
	h := javadist.Platform{}
	musl := javadist.Platform{OS: "linux", Arch: "x64", Libc: "musl"}
	srv := fakejdk.New(t,
		microsoft(ver(17, 14, 0), h),
		microsoft(javaversion.Version{Major: 17, Security: 2, Build: 8, Revision: 1}, h),
		microsoft(ver(21, 5, 0), h),
		microsoft(ver(21, 6, 0), h),
		microsoft(ver(25, 3, 0), h),
		microsoft(ver(21, 4, 0), musl),
	)
	tests := []struct {
		name     string
		spec     string
		platform javadist.Platform
		want     string
	}{
		{name: "major", spec: "21", want: "21.0.6"},
		{name: "exact", spec: "21.0.5", want: "21.0.5"},
		{name: "build and revision", spec: "17.0.2", want: "17.0.2+8.1"},
		{name: "latest", spec: "latest", want: "25.0.3"},
		{name: "range across majors", spec: ">=17 <25", want: "21.0.6"},
		{name: "musl", spec: "21", platform: musl, want: "21.0.4"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.platform
			if p == (javadist.Platform{}) {
				p = host(t)
			}
			r, err := resolveFrom(t, "microsoft", srv, tc.spec, p, "jdk")
			assert.NilError(t, err)
			assert.Equal(t, r.Version.String(), tc.want)
			assert.Equal(t, r.SHA256, fakejdk.Checksum(microsoft(r.Version, p)), "the checksum comes from the file beside the archive")
		})
	}
}

func TestMicrosoft_Resolve_Errors(t *testing.T) {
	srv := fakejdk.New(t, microsoft(ver(21, 6, 0), javadist.Platform{}))
	arm := javadist.Platform{OS: "linux", Arch: "armv7", Libc: "glibc"}

	_, err := resolveFrom(t, "microsoft", srv, "21", arm, "jdk")
	assert.ErrorContains(t, err, fmt.Sprintf("Microsoft publishes no jdk releases for %s", arm))
	_, err = resolveFrom(t, "microsoft", srv, "17", host(t), "jdk")
	assert.ErrorContains(t, err, "no matching Java version")
}
