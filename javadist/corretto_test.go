package javadist_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/setup-java/internal/testing/fakejdk"
	"github.com/circleci-functions/setup-java/javadist"
	"github.com/circleci-functions/setup-java/javaversion"
)

// corretto is a build with Amazon's revision 1, as nearly every one has.
func corretto(major, security, build int, p javadist.Platform) fakejdk.Build {
	return fakejdk.Build{
		Distribution: "corretto",
		Version:      javaversion.Version{Major: major, Security: security, Build: build, Revision: 1},
		Platform:     p,
	}
}

func TestCorretto_Resolve(t *testing.T) {
	h := javadist.Platform{}
	musl := javadist.Platform{OS: "linux", Arch: "x64", Libc: "musl"}
	srv := fakejdk.New(t,
		corretto(8, 504, 4, h),
		corretto(17, 20, 12, h),
		corretto(21, 11, 9, h),
		corretto(21, 12, 12, h),
		corretto(25, 4, 10, h),
		corretto(21, 12, 12, musl),
	)
	tests := []struct {
		name     string
		spec     string
		platform javadist.Platform
		want     string
	}{
		{name: "major", spec: "21", want: "21.0.12+12.1"},
		{name: "newest version exactly", spec: "21.0.12", want: "21.0.12+12.1"},
		{name: "java 8", spec: "8", want: "8.0.504+4.1"},
		{name: "latest", spec: "latest", want: "25.0.4+10.1"},
		{name: "range across majors", spec: ">=17 <25", want: "21.0.12+12.1"},
		{name: "musl", spec: "21", platform: musl, want: "21.0.12+12.1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.platform
			if p == (javadist.Platform{}) {
				p = host(t)
			}
			r, err := resolveFrom(t, "corretto", srv, tc.spec, p, "jdk")
			assert.NilError(t, err)
			assert.Equal(t, r.Version.String(), tc.want)
			assert.Assert(t, len(r.SHA256) == 64, r.SHA256)
		})
	}
}

func TestCorretto_Resolve_Errors(t *testing.T) {
	srv := fakejdk.New(t, corretto(21, 12, 12, javadist.Platform{}))
	tests := []struct {
		name string
		spec string
		pkg  string
		want string
	}{
		{name: "an older build", spec: "21.0.11", pkg: "jdk", want: "only the newest of each version"},
		{name: "no jre", spec: "21", pkg: "jre", want: fmt.Sprintf("Corretto publishes no jre releases for %s", host(t))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveFrom(t, "corretto", srv, tc.spec, host(t), tc.pkg)
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestCorretto_Install(t *testing.T) {
	srv := fakejdk.New(t, corretto(21, 12, 12, javadist.Platform{}))
	r, err := resolveFrom(t, "corretto", srv, "21", host(t), "jdk")
	assert.NilError(t, err)

	dest := filepath.Join(t.TempDir(), "jdk")
	assert.NilError(t, javadist.Install(context.Background(), r, dest), "the download path in the index is relative to the download site")
	assert.Assert(t, javadist.Installed(dest))
	_, err = os.Stat(filepath.Join(javadist.Home(dest), "release"))
	assert.NilError(t, err)
}
