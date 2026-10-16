package javadist_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/setup-java/internal/testing/fakejdk"
	"github.com/circleci-functions/setup-java/javadist"
	"github.com/circleci-functions/setup-java/javaversion"
)

func openjdk(v javaversion.Version) fakejdk.Build {
	return fakejdk.Build{Distribution: "oracle-openjdk", Version: v}
}

func TestOracleOpenJDK_Resolve(t *testing.T) {
	srv := fakejdk.New(t,
		openjdk(ver(17, 2, 8)),
		openjdk(ver(21, 0, 35)),
		openjdk(ver(21, 1, 12)),
		openjdk(ver(21, 2, 13)),
		openjdk(ver(27, 0, 35)),
	)
	tests := []struct {
		name string
		spec string
		want string
	}{
		{name: "major", spec: "21", want: "21.0.2+13"},
		{name: "older version from the archive", spec: "21.0.1", want: "21.0.1+12"},
		{name: "first release", spec: "21+35", want: "21.0.0+35"},
		{name: "latest", spec: "latest", want: "27.0.0+35"},
		{name: "range across majors", spec: ">=17 <27", want: "21.0.2+13"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, err := resolveFrom(t, "oracle-openjdk", srv, tc.spec, host(t), "jdk")
			assert.NilError(t, err)
			assert.Equal(t, r.Version.String(), tc.want)
			want := openjdk(r.Version)
			want.Platform = host(t)
			assert.Equal(t, r.SHA256, fakejdk.Checksum(want), "the checksum comes from the .sha256 file beside the archive")
		})
	}
}

func TestOracleOpenJDK_Resolve_Errors(t *testing.T) {
	srv := fakejdk.New(t, openjdk(ver(21, 2, 13)))
	musl := javadist.Platform{OS: "linux", Arch: "x64", Libc: "musl"}

	_, err := resolveFrom(t, "oracle-openjdk", srv, "21", musl, "jdk")
	assert.ErrorContains(t, err, fmt.Sprintf("Oracle OpenJDK publishes no jdk releases for %s", musl))
	_, err = resolveFrom(t, "oracle-openjdk", srv, "21.0.5", host(t), "jdk")
	assert.ErrorContains(t, err, "no matching Java version")
}

func TestOracleOpenJDK_Install(t *testing.T) {
	srv := fakejdk.New(t, openjdk(ver(21, 2, 13)))
	r, err := resolveFrom(t, "oracle-openjdk", srv, "21", host(t), "jdk")
	assert.NilError(t, err)

	dest := filepath.Join(t.TempDir(), "jdk")
	assert.NilError(t, javadist.Install(context.Background(), r, dest))
	assert.Assert(t, javadist.Installed(dest))
}
