package javadist_test

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/setup-java/internal/testing/fakeadoptium"
	"github.com/circleci-functions/setup-java/javadist"
	"github.com/circleci-functions/setup-java/javaversion"
)

func ver(major, security, build int) javaversion.Version {
	return javaversion.Version{Major: major, Security: security, Build: build}
}

func host(t *testing.T) javadist.Platform {
	t.Helper()
	p, err := javadist.Host("")
	assert.NilError(t, err)
	return p
}

func resolve(t *testing.T, srv *fakeadoptium.Server, raw string, p javadist.Platform, pkg string) (javadist.Release, error) {
	t.Helper()
	spec, err := javaversion.Parse(raw)
	assert.NilError(t, err)
	return javadist.Temurin{BaseURL: srv.URL}.Resolve(context.Background(), spec, p, pkg)
}

func TestTemurin_Resolve(t *testing.T) {
	musl := javadist.Platform{OS: "linux", Arch: "x64", Libc: "musl"}
	arm := javadist.Platform{OS: "linux", Arch: "armv7", Libc: "glibc"}
	srv := fakeadoptium.New(t,
		fakeadoptium.Build{Version: ver(17, 14, 7)},
		fakeadoptium.Build{Version: ver(21, 5, 11)},
		fakeadoptium.Build{Version: ver(21, 6, 7)},
		fakeadoptium.Build{Version: ver(21, 6, 7), Package: "jre"},
		fakeadoptium.Build{Version: ver(25, 1, 8)},
		fakeadoptium.Build{Version: javaversion.Version{Major: 28, Build: 19, EA: true}},
		fakeadoptium.Build{Version: ver(21, 4, 7), Platform: musl},
		fakeadoptium.Build{Version: ver(17, 1, 12), Platform: arm},
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
		{name: "early access", spec: "28-ea", want: "28-ea+19"},
		{name: "jre", spec: "21", pkg: "jre", want: "21.0.6+7"},
		{name: "musl builds for a musl platform", spec: "21", platform: musl, want: "21.0.4+7"},
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
			r, err := resolve(t, srv, tc.spec, p, pkg)
			assert.NilError(t, err)
			assert.Equal(t, r.Version.String(), tc.want)
			assert.Assert(t, strings.HasPrefix(r.URL, srv.URL+"/download/"), r.URL)
			assert.Assert(t, len(r.SHA256) == 64, r.SHA256)
		})
	}
}

func TestTemurin_Resolve_QueriesOneMajor(t *testing.T) {
	srv := fakeadoptium.New(t, fakeadoptium.Build{Version: ver(21, 6, 7)})
	_, err := resolve(t, srv, "21", host(t), "jdk")
	assert.NilError(t, err)

	reqs := srv.Requests()
	assert.Equal(t, len(reqs), 1, "%v", reqs)
	u, err := url.Parse(reqs[0])
	assert.NilError(t, err)
	assert.Equal(t, u.Path, "/v3/assets/version/[21,22)")
	assert.Equal(t, u.Query().Get("release_type"), "ga")
	assert.Equal(t, u.Query().Get("image_type"), "jdk")
}

func TestTemurin_Resolve_Pages(t *testing.T) {
	var builds []fakeadoptium.Build
	for i := 1; i <= 45; i++ {
		builds = append(builds, fakeadoptium.Build{Version: ver(17, i, 1)})
	}
	srv := fakeadoptium.New(t, builds...)

	r, err := resolve(t, srv, "17.0.1", host(t), "jdk")
	assert.NilError(t, err, "the oldest build is on the third page")
	assert.Equal(t, r.Version.String(), "17.0.1+1")
	assert.Equal(t, len(srv.Requests()), 3)
}

func TestTemurin_Resolve_Errors(t *testing.T) {
	srv := fakeadoptium.New(t, fakeadoptium.Build{Version: ver(21, 6, 7)})
	tests := []struct {
		name string
		spec string
		want string
	}{
		{name: "no builds of that major", spec: "17", want: fmt.Sprintf("Temurin publishes no jdk releases for %s", host(t))},
		{name: "no such version", spec: "21.0.5", want: "no matching Java version"},
		{name: "no early access", spec: "21-ea", want: "early-access builds"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolve(t, srv, tc.spec, host(t), "jdk")
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestCheckPackage(t *testing.T) {
	assert.NilError(t, javadist.CheckPackage(javadist.Temurin{}, "jre"))
	assert.ErrorContains(t, javadist.CheckPackage(javadist.Temurin{}, "jdk+fx"), `temurin does not publish a "jdk+fx" package: use one of jdk, jre`)
}
