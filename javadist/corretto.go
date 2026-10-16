package javadist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/circleci-functions/setup-java/javaversion"
)

const (
	// CorrettoIndexURL lists the newest Amazon Corretto build of each version.
	CorrettoIndexURL = "https://corretto.github.io/corretto-downloads/latest_links/indexmap_with_checksum.json"
	// CorrettoDownloadURL is where the index's download paths are relative to.
	CorrettoDownloadURL = "https://corretto.aws"
)

// Corretto is Amazon Corretto. Amazon lists only the newest build of each
// version, so an older one cannot be installed.
type Corretto struct {
	IndexURL    string
	DownloadURL string
}

func newCorretto(apiURL string) Distribution {
	if apiURL == "" {
		return Corretto{IndexURL: CorrettoIndexURL, DownloadURL: CorrettoDownloadURL}
	}
	return Corretto{IndexURL: apiURL + "/corretto-downloads/latest_links/indexmap_with_checksum.json", DownloadURL: apiURL}
}

func (Corretto) Name() string { return "corretto" }

func (Corretto) Packages() []string { return []string{"jdk", "jre"} }

// correttoBuilds is one operating system's part of the index: arch, then
// package, then major, then file type. The index has other top-level entries
// shaped differently, such as for Mission Control, so each is decoded only
// when needed.
type correttoBuilds map[string]map[string]map[string]map[string]struct {
	SHA256   string `json:"checksum_sha256"`
	Resource string `json:"resource"`
}

func (c Corretto) Resolve(ctx context.Context, spec javaversion.Spec, p Platform, pkg string) (Release, error) {
	var index map[string]json.RawMessage
	if _, err := getJSON(ctx, newClient(ctx, "corretto", ""), c.IndexURL, nil, &index); err != nil {
		return Release{}, fmt.Errorf("list Corretto builds: %w", err)
	}
	var forOS correttoBuilds
	if raw, ok := index[correttoOS(p)]; ok {
		if err := json.Unmarshal(raw, &forOS); err != nil {
			return Release{}, fmt.Errorf("list Corretto builds: %w", err)
		}
	}
	majors := forOS[correttoArch(p.Arch)][pkg]
	ext := strings.TrimPrefix(p.ArchiveExt(), ".")

	var builds []Release
	for _, files := range majors {
		file, ok := files[ext]
		if !ok {
			continue
		}
		v, err := correttoVersion(file.Resource)
		if err != nil {
			return Release{}, err
		}
		builds = append(builds, Release{Version: v, URL: c.DownloadURL + file.Resource, SHA256: file.SHA256})
	}
	if len(builds) == 0 {
		return Release{}, errors.New(noBuilds("Corretto", pkg, p, false))
	}
	r, err := pick(spec, builds)
	if err != nil {
		return Release{}, fmt.Errorf("%w in Corretto's %s builds for %s, which are only the newest of each version", err, pkg, p)
	}
	return r, nil
}

// correttoVersion reads the version from a download path such as
// /downloads/resources/21.0.12.12.1/amazon-corretto-21.0.12.12.1-linux-x64.tar.gz:
// the Java version, the build and Amazon's revision. Java 8's is
// 8.<update>.<build>.<revision>.
func correttoVersion(resource string) (javaversion.Version, error) {
	_, rest, _ := strings.Cut(resource, "/resources/")
	raw, _, _ := strings.Cut(rest, "/")
	var n []int
	for f := range strings.SplitSeq(raw, ".") {
		i, err := strconv.Atoi(f)
		if err != nil {
			return javaversion.Version{}, fmt.Errorf("unrecognised Corretto version %q in %s", raw, resource)
		}
		n = append(n, i)
	}
	switch {
	case len(n) == 4 && n[0] == 8:
		return javaversion.Version{Major: 8, Security: n[1], Build: n[2], Revision: n[3]}, nil
	case len(n) == 5:
		return javaversion.Version{Major: n[0], Minor: n[1], Security: n[2], Build: n[3], Revision: n[4]}, nil
	case len(n) == 6:
		return javaversion.Version{Major: n[0], Minor: n[1], Security: n[2], Patch: n[3], Build: n[4], Revision: n[5]}, nil
	}
	return javaversion.Version{}, fmt.Errorf("unrecognised Corretto version %q in %s", raw, resource)
}

func correttoOS(p Platform) string {
	if p.Libc == "musl" {
		return "alpine"
	}
	return p.OS
}

func correttoArch(arch string) string {
	if arch == "armv7" {
		return "arm"
	}
	return arch
}
