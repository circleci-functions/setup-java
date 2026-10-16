package javadist

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

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

func (t Temurin) Resolve(ctx context.Context, spec javaversion.Spec, p Platform, pkg string) (Release, error) {
	client := hc.New(ctx, hc.Config{Name: "adoptium", BaseURL: t.BaseURL, Timeout: 2 * time.Minute})
	rng, err := t.versionRange(ctx, client, spec)
	if err != nil {
		return Release{}, err
	}
	assets, err := t.assets(ctx, client, rng, spec.EA(), p, pkg)
	if err != nil {
		return Release{}, err
	}

	byVersion := map[string]adoptiumAsset{}
	versions := make([]javaversion.Version, 0, len(assets))
	for _, a := range assets {
		if len(a.Binaries) == 0 {
			continue
		}
		v := a.version()
		byVersion[v.String()] = a
		versions = append(versions, v)
	}
	v, err := spec.Resolve(versions)
	if err != nil {
		return Release{}, fmt.Errorf("%w in Temurin's %s builds for %s", err, pkg, p)
	}
	bin := byVersion[v.String()].Binaries[0].Package
	return Release{Version: v, URL: bin.Link, SHA256: bin.Checksum}, nil
}

// versionRange returns the Adoptium version range to list: one major when
// spec fixes it or is latest, so a few requests cover it, and every release
// otherwise.
func (t Temurin) versionRange(ctx context.Context, client *hc.Client, spec javaversion.Spec) (string, error) {
	major, ok := spec.Major()
	if !ok && spec.Latest() {
		var info struct {
			MostRecentFeatureRelease int `json:"most_recent_feature_release"`
		}
		if err := client.Call(ctx, hc.NewRequest(http.MethodGet, "/v3/info/available_releases", hc.JSONDecoder(&info))); err != nil {
			return "", fmt.Errorf("look up the latest Temurin release: %w", err)
		}
		major, ok = info.MostRecentFeatureRelease, info.MostRecentFeatureRelease > 0
	}
	if !ok {
		return "[1.0,100.0]", nil
	}
	return fmt.Sprintf("[%d,%d)", major, major+1), nil
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
		err := client.Call(ctx, hc.NewRequest(http.MethodGet, "/v3/assets/version/%s",
			hc.RouteParams(url.PathEscape(rng)),
			hc.QueryParams(params),
			hc.JSONDecoder(&batch),
		))
		if hc.HasStatusCode(err, http.StatusNotFound) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list Temurin builds: %w", err)
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

func noBuilds(vendor, pkg string, p Platform, ea bool) string {
	kind := "releases"
	if ea {
		kind = "early-access builds"
	}
	return fmt.Sprintf("%s publishes no %s %s for %s", vendor, pkg, kind, p)
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
