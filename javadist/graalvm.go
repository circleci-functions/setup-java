package javadist

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"

	hc "github.com/circleci-functions/toolkit/httpcl"

	"github.com/circleci-functions/setup-java/javaversion"
)

// GraalVMURL is where Oracle publishes Oracle GraalVM. It has no list of
// builds: each major's newest build has a fixed "latest" link, and every
// release has a link of its own in the archive.
const GraalVMURL = "https://download.oracle.com/graalvm"

const (
	graalVMName   = "Oracle GraalVM"
	graalVMOldest = 17
)

// GraalVM is Oracle GraalVM.
type GraalVM struct {
	BaseURL string
	// AdoptiumURL is asked for the newest Java release, which latest
	// installs.
	AdoptiumURL string
}

func newGraalVM(apiURL string) Distribution {
	if apiURL == "" {
		return GraalVM{BaseURL: GraalVMURL, AdoptiumURL: AdoptiumURL}
	}
	return GraalVM{BaseURL: apiURL + "/graalvm", AdoptiumURL: apiURL}
}

func (GraalVM) Name() string { return "graalvm" }

func (GraalVM) Packages() []string { return []string{"jdk"} }

// Env points GRAALVM_HOME at the JDK, as GraalVM's own tools expect.
func (GraalVM) Env(home string) map[string]string {
	return map[string]string{"GRAALVM_HOME": home}
}

// Resolve builds the download link from spec. A major alone links to that
// major's newest build, which Oracle replaces in place, so the release is
// floating: its version is known only once it is unpacked.
func (g GraalVM) Resolve(ctx context.Context, spec javaversion.Spec, p Platform, pkg string) (Release, error) {
	if p.Libc == "musl" || (p.Arch != "x64" && p.Arch != "aarch64") {
		return Release{}, errors.New(noBuilds(graalVMName, pkg, p, false))
	}
	major, err := g.major(ctx, spec)
	if err != nil {
		return Release{}, err
	}
	r, err := g.link(spec, major, p)
	if err != nil {
		return Release{}, err
	}

	found, err := reachable(ctx, r.URL)
	switch {
	case err != nil:
		return Release{}, fmt.Errorf("look up %s %s: %w", graalVMName, spec, err)
	case !found && spec.Latest():
		return Release{}, fmt.Errorf("%s has no build of Java %d, the newest Java release, yet: ask for a version such as 21", graalVMName, major)
	case !found:
		return Release{}, fmt.Errorf("%w for %q: %s publishes no %s", javaversion.ErrNoMatch, spec.String(), graalVMName, path.Base(r.URL))
	}
	if r.SHA256, err = fetchSHA256(ctx, r.URL+".sha256"); err != nil {
		return Release{}, err
	}
	if r.SHA256 == "" && r.Floating {
		return Release{}, fmt.Errorf("%s publishes no checksum for %s, so it cannot be told apart from an earlier build", graalVMName, r.URL)
	}
	return r, nil
}

// major returns the major spec asks for: the newest Java release for latest.
func (g GraalVM) major(ctx context.Context, spec javaversion.Spec) (int, error) {
	if spec.Latest() {
		return latestFeatureRelease(ctx, newClient(ctx, "adoptium", g.AdoptiumURL))
	}
	_, exact := spec.Exact()
	major, ok := spec.Major()
	switch {
	case !ok || exact:
		return 0, fmt.Errorf("%s takes a major version such as 21, a full version such as 21.0.5, or latest, not %q", graalVMName, spec)
	case major < graalVMOldest:
		return 0, fmt.Errorf("%s starts at Java %d, not %d", graalVMName, graalVMOldest, major)
	}
	return major, nil
}

// link returns the release spec links to: a major's floating latest link, or
// a full version's archive link.
func (g GraalVM) link(spec javaversion.Spec, major int, p Platform) (Release, error) {
	file := func(version string) string {
		return fmt.Sprintf("graalvm-jdk-%s_%s-%s_bin%s", version, p.OS, p.Arch, p.ArchiveExt())
	}
	if spec.Latest() || !strings.Contains(spec.String(), ".") {
		return Release{
			Version:  javaversion.Version{Major: major},
			URL:      fmt.Sprintf("%s/%d/latest/%s", g.BaseURL, major, file(strconv.Itoa(major))),
			Floating: true,
		}, nil
	}
	v, err := javaversion.ParseVersion(spec.String())
	if err != nil {
		return Release{}, err
	}
	return Release{Version: v, URL: fmt.Sprintf("%s/%d/archive/%s", g.BaseURL, major, file(spec.String()))}, nil
}

// reachable reports whether url answers a HEAD request, false on a 404.
func reachable(ctx context.Context, url string) (bool, error) {
	err := newClient(ctx, "graalvm", "").Call(ctx, hc.NewRequest(http.MethodHead, "%s", hc.RouteParams(url)))
	if hc.HasStatusCode(err, http.StatusNotFound) {
		return false, nil
	}
	return err == nil, err
}
