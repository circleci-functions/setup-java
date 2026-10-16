package javadist

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	hc "github.com/circleci-functions/toolkit/httpcl"

	"github.com/circleci-functions/setup-java/javaversion"
)

// AzulURL is the Azul metadata API, which lists Zulu builds.
const AzulURL = "https://api.azul.com"

const (
	azulPageSize = 100
	azulMaxPages = 20
)

// Zulu is Azul Zulu.
type Zulu struct {
	BaseURL string
}

func (Zulu) Name() string { return "zulu" }

func (Zulu) Packages() []string {
	return []string{"jdk", "jre", "jdk+fx", "jre+fx", "jdk+crac", "jre+crac"}
}

type azulPackage struct {
	DownloadURL   string `json:"download_url"`
	JavaVersion   []int  `json:"java_version"`
	DistroVersion []int  `json:"distro_version"`
	BuildNumber   int    `json:"openjdk_build_number"`
	UUID          string `json:"package_uuid"`
}

func (a azulPackage) version() (javaversion.Version, bool) {
	if len(a.JavaVersion) < 3 {
		return javaversion.Version{}, false
	}
	v := javaversion.Version{Major: a.JavaVersion[0], Minor: a.JavaVersion[1], Security: a.JavaVersion[2], Build: a.BuildNumber}
	if len(a.JavaVersion) > 3 {
		v.Patch = a.JavaVersion[3]
	}
	return v, true
}

// Resolve lists Zulu builds of pkg for p, newest Zulu build of each Java
// version only, and looks up the SHA-256 of the one it picks: Azul's list
// leaves it out.
func (z Zulu) Resolve(ctx context.Context, spec javaversion.Spec, p Platform, pkg string) (Release, error) {
	client := newClient(ctx, "azul", z.BaseURL)
	packages, err := z.list(ctx, client, spec, p, pkg)
	if err != nil {
		return Release{}, err
	}

	newest := map[javaversion.Version]azulPackage{}
	for _, a := range packages {
		v, ok := a.version()
		if !ok {
			continue
		}
		if seen, ok := newest[v]; !ok || slices.Compare(a.DistroVersion, seen.DistroVersion) > 0 {
			newest[v] = a
		}
	}
	builds := make([]Release, 0, len(newest))
	for v, a := range newest {
		builds = append(builds, Release{Version: v, URL: a.DownloadURL})
	}
	r, err := pick(spec, builds)
	if err != nil {
		return Release{}, fmt.Errorf("%w in Zulu's %s builds for %s", err, pkg, p)
	}

	var details struct {
		SHA256 string `json:"sha256_hash"`
	}
	if _, err := getJSON(ctx, client, "/metadata/v1/zulu/packages/"+newest[r.Version].UUID, nil, &details); err != nil {
		return Release{}, fmt.Errorf("look up the checksum of Zulu %s: %w", r.Version, err)
	}
	r.SHA256 = details.SHA256
	return r, nil
}

// list returns the Zulu packages of pkg for p: of one major when spec fixes
// it, the newest of each major for latest, and every one otherwise.
func (Zulu) list(ctx context.Context, client *hc.Client, spec javaversion.Spec, p Platform, pkg string) ([]azulPackage, error) {
	kind, extra, _ := strings.Cut(pkg, "+")
	params := map[string]string{
		"os":                 azulOS(p),
		"arch":               azulArch(p.Arch),
		"archive_type":       strings.TrimPrefix(p.ArchiveExt(), "."),
		"java_package_type":  kind,
		"javafx_bundled":     strconv.FormatBool(extra == "fx"),
		"crac_supported":     strconv.FormatBool(extra == "crac"),
		"release_status":     "ga",
		"availability_types": "ca",
		"page_size":          strconv.Itoa(azulPageSize),
	}
	if major, ok := spec.Major(); ok {
		params["java_version"] = strconv.Itoa(major)
	} else if spec.Latest() {
		params["latest"] = "true"
	}

	var all []azulPackage
	for page := 1; page <= azulMaxPages; page++ {
		params["page"] = strconv.Itoa(page)
		var batch []azulPackage
		found, err := getJSON(ctx, client, "/metadata/v1/zulu/packages/", params, &batch)
		if err != nil {
			return nil, fmt.Errorf("list Zulu builds: %w", err)
		}
		if !found {
			break
		}
		all = append(all, batch...)
		if len(batch) < azulPageSize {
			break
		}
	}
	if len(all) == 0 {
		return nil, errors.New(noBuilds("Zulu", pkg, p, false))
	}
	return all, nil
}

func azulOS(p Platform) string {
	switch {
	case p.OS != "linux":
		return p.OS
	case p.Libc == "musl":
		return "linux_musl"
	}
	return "linux_glibc"
}

func azulArch(arch string) string {
	switch arch {
	case "x86":
		return "i686"
	case "armv7":
		return "aarch32hf"
	}
	return arch
}
