package javadist

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/circleci-functions/setup-java/javaversion"
)

// MicrosoftManifestURL lists the Microsoft Build of OpenJDK releases.
const MicrosoftManifestURL = "https://aka.ms/download-jdk/microsoft-openjdk-versions.json"

// Microsoft is the Microsoft Build of OpenJDK.
type Microsoft struct {
	ManifestURL string
}

func newMicrosoft(apiURL string) Distribution {
	if apiURL == "" {
		return Microsoft{ManifestURL: MicrosoftManifestURL}
	}
	return Microsoft{ManifestURL: apiURL + "/download-jdk/microsoft-openjdk-versions.json"}
}

func (Microsoft) Name() string { return "microsoft" }

func (Microsoft) Packages() []string { return []string{"jdk"} }

type microsoftRelease struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
	Files   []struct {
		Filename    string `json:"filename"`
		Arch        string `json:"arch"`
		Platform    string `json:"platform"`
		DownloadURL string `json:"download_url"`
	} `json:"files"`
}

// Resolve picks from the manifest, then reads the SHA-256 from the file
// Microsoft publishes beside each archive.
func (m Microsoft) Resolve(ctx context.Context, spec javaversion.Spec, p Platform, pkg string) (Release, error) {
	var manifest []microsoftRelease
	if _, err := getJSON(ctx, newClient(ctx, "microsoft", ""), m.ManifestURL, nil, &manifest); err != nil {
		return Release{}, fmt.Errorf("list Microsoft builds: %w", err)
	}

	var builds []Release
	for _, rel := range manifest {
		if !rel.Stable {
			continue
		}
		v, err := javaversion.ParseVersion(rel.Version)
		if err != nil {
			continue
		}
		for _, f := range rel.Files {
			if microsoftPlatforms(p)[f.Platform] && f.Arch == p.Arch && strings.HasSuffix(f.Filename, p.ArchiveExt()) {
				builds = append(builds, Release{Version: v, URL: f.DownloadURL})
				break
			}
		}
	}
	if len(builds) == 0 {
		return Release{}, errors.New(noBuilds("Microsoft", pkg, p, false))
	}
	r, err := pick(spec, builds)
	if err != nil {
		return Release{}, fmt.Errorf("%w in Microsoft's %s builds for %s", err, pkg, p)
	}
	if r.SHA256, err = fetchSHA256(ctx, r.URL+".sha256sum.txt"); err != nil {
		return Release{}, err
	}
	return r, nil
}

// microsoftPlatforms are the manifest's names for p. It calls macOS both
// darwin and macos.
func microsoftPlatforms(p Platform) map[string]bool {
	switch {
	case p.OS == "macos":
		return map[string]bool{"darwin": true, "macos": true}
	case p.OS == "windows":
		return map[string]bool{"win32": true}
	case p.Libc == "musl":
		return map[string]bool{"alpine": true}
	}
	return map[string]bool{"linux": true}
}
