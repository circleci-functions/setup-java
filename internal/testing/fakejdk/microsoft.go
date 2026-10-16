package fakejdk

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/circleci-functions/setup-java/javadist"
)

const microsoftManifest = "/download-jdk/microsoft-openjdk-versions.json"

// serveMicrosoft serves the manifest, newest release first, and the
// .sha256sum.txt file Microsoft publishes beside each archive.
func serveMicrosoft(s *Server, w http.ResponseWriter, r *http.Request) bool {
	builds := s.of("microsoft")
	if name, ok := strings.CutSuffix(r.URL.Path, ".sha256sum.txt"); ok && strings.HasPrefix(name, "/download/") {
		for _, b := range builds {
			if "/download/"+ArchiveName(b) == name {
				_, _ = fmt.Fprintf(w, "%s  %s\n", Checksum(b), ArchiveName(b))
				return true
			}
		}
		http.NotFound(w, r)
		return true
	}
	if r.URL.Path != microsoftManifest {
		return false
	}

	slices.SortFunc(builds, func(a, b Build) int { return b.Version.Compare(a.Version) })
	var releases []map[string]any
	byVersion := map[string]int{}
	for _, b := range builds {
		v := b.Version.String()
		i, ok := byVersion[v]
		if !ok {
			i = len(releases)
			byVersion[v] = i
			releases = append(releases, map[string]any{"version": v, "stable": true, "files": []map[string]string{}})
		}
		releases[i]["files"] = append(releases[i]["files"].([]map[string]string), map[string]string{
			"filename":     ArchiveName(b),
			"arch":         b.Platform.Arch,
			"platform":     microsoftPlatform(b.Platform),
			"download_url": s.downloadURL(b),
		})
	}
	writeJSON(w, releases)
	return true
}

func microsoftPlatform(p javadist.Platform) string {
	switch {
	case p.OS == "macos":
		return "darwin"
	case p.OS == "windows":
		return "win32"
	case p.Libc == "musl":
		return "alpine"
	}
	return "linux"
}
