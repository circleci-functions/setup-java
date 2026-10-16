package javadist

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	hc "github.com/circleci-functions/toolkit/httpcl"

	"github.com/circleci-functions/setup-java/javaversion"
)

// AdoptiumURL is the Adoptium API, which lists Eclipse Temurin builds.
const AdoptiumURL = "https://api.adoptium.net"

const (
	adoptiumPageSize = 20
	adoptiumMaxPages = 50
)

// Temurin is Eclipse Temurin, from the Adoptium project.
type Temurin struct {
	BaseURL string
}

func (Temurin) Name() string { return "temurin" }

func (Temurin) Packages() []string { return []string{"jdk", "jre"} }

type adoptiumAsset struct {
	Binaries []struct {
		Package struct {
			Checksum string `json:"checksum"`
			Link     string `json:"link"`
			Name     string `json:"name"`
		} `json:"package"`
	} `json:"binaries"`
	VersionData struct {
		Major    int    `json:"major"`
		Minor    int    `json:"minor"`
		Security int    `json:"security"`
		Patch    int    `json:"patch"`
		Build    int    `json:"build"`
		Optional string `json:"optional"`
		Pre      string `json:"pre"`
	} `json:"version_data"`
}

func (a adoptiumAsset) version() javaversion.Version {
	d := a.VersionData
	return javaversion.Version{
		Major:    d.Major,
		Minor:    d.Minor,
		Security: d.Security,
		Patch:    d.Patch,
		Build:    d.Build,
		EA:       d.Optional == "ea" || d.Pre != "",
	}
}

func (Temurin) EarlyAccess() bool { return true }

func (t Temurin) Resolve(ctx context.Context, spec javaversion.Spec, p Platform, pkg string) (Release, error) {
	client := newClient(ctx, "adoptium", t.BaseURL)
	rng, err := t.versionRange(ctx, client, spec)
	if err != nil {
		return Release{}, err
	}
	assets, err := t.assets(ctx, client, rng, spec.EA(), p, pkg)
	if err != nil {
		return Release{}, err
	}

	builds := make([]Release, 0, len(assets))
	for _, a := range assets {
		if len(a.Binaries) == 0 {
			continue
		}
		bin := a.Binaries[0].Package
		builds = append(builds, Release{Version: a.version(), URL: bin.Link, SHA256: bin.Checksum})
	}
	r, err := pick(spec, builds)
	if err != nil {
		return Release{}, fmt.Errorf("%w in Temurin's %s builds for %s", err, pkg, p)
	}
	return r, nil
}

// versionRange returns the Adoptium version range to list: one major when
// spec fixes it or is latest, so a few requests cover it, and every release
// otherwise.
func (t Temurin) versionRange(ctx context.Context, client *hc.Client, spec javaversion.Spec) (string, error) {
	major, ok := spec.Major()
	if !ok && spec.Latest() {
		latest, err := latestFeatureRelease(ctx, client)
		if err != nil {
			return "", err
		}
		major, ok = latest, true
	}
	if !ok {
		return "[1.0,100.0]", nil
	}
	return fmt.Sprintf("[%d,%d)", major, major+1), nil
}

// latestFeatureRelease asks the Adoptium API at client for the newest Java
// major released.
func latestFeatureRelease(ctx context.Context, client *hc.Client) (int, error) {
	var info struct {
		MostRecentFeatureRelease int `json:"most_recent_feature_release"`
	}
	found, err := getJSON(ctx, client, "/v3/info/available_releases", nil, &info)
	if err != nil {
		return 0, fmt.Errorf("look up the latest Java release: %w", err)
	}
	if !found || info.MostRecentFeatureRelease <= 0 {
		return 0, errors.New("look up the latest Java release: Adoptium reported none")
	}
	return info.MostRecentFeatureRelease, nil
}

// assets lists the builds in rng for p, newest first, a page at a time until
// Adoptium runs out. It answers a page past the end with a 404.
func (t Temurin) assets(ctx context.Context, client *hc.Client, rng string, ea bool, p Platform, pkg string) ([]adoptiumAsset, error) {
	releaseType := "ga"
	if ea {
		releaseType = "ea"
	}
	params := map[string]string{
		"project":      "jdk",
		"vendor":       "eclipse",
		"jvm_impl":     "hotspot",
		"heap_size":    "normal",
		"sort_method":  "DEFAULT",
		"sort_order":   "DESC",
		"os":           adoptiumOS(p),
		"architecture": adoptiumArch(p.Arch),
		"image_type":   pkg,
		"release_type": releaseType,
		"page_size":    strconv.Itoa(adoptiumPageSize),
	}

	var all []adoptiumAsset
	for page := range adoptiumMaxPages {
		params["page"] = strconv.Itoa(page)
		var batch []adoptiumAsset
		found, err := getJSON(ctx, client, "/v3/assets/version/"+url.PathEscape(rng), params, &batch)
		if err != nil {
			return nil, fmt.Errorf("list Temurin builds: %w", err)
		}
		if !found {
			break
		}
		all = append(all, batch...)
		if len(batch) < adoptiumPageSize {
			break
		}
	}
	if len(all) == 0 {
		return nil, errors.New(noBuilds("Temurin", pkg, p, ea))
	}
	return all, nil
}

func adoptiumOS(p Platform) string {
	switch {
	case p.OS == "macos":
		return "mac"
	case p.Libc == "musl":
		return "alpine-linux"
	}
	return p.OS
}

func adoptiumArch(arch string) string {
	if arch == "armv7" {
		return "arm"
	}
	return arch
}
