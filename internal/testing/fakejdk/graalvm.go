package fakejdk

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// serveGraalVM serves Oracle's GraalVM downloads: each major's newest build
// at its latest link, and every build at its archive link, each with a
// .sha256 file beside it.
func serveGraalVM(s *Server, w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/graalvm/") {
		return false
	}
	builds := s.of("graalvm")
	slices.SortFunc(builds, func(a, b Build) int { return b.Version.Compare(a.Version) })

	path, checksum := strings.CutSuffix(r.URL.Path, ".sha256")
	for _, b := range builds {
		if path != graalVMArchivePath(b) && path != graalVMLatestPath(b) {
			continue
		}
		if path == graalVMLatestPath(b) && !isNewest(b, builds) {
			continue
		}
		if checksum {
			_, _ = fmt.Fprintf(w, "%s\n", Checksum(b))
			return true
		}
		_, _ = w.Write(Archive(b))
		return true
	}
	http.NotFound(w, r)
	return true
}

func isNewest(b Build, newestFirst []Build) bool {
	for _, other := range newestFirst {
		if other.Version.Major == b.Version.Major && other.Platform == b.Platform {
			return other == b
		}
	}
	return false
}

func graalVMLatestPath(b Build) string {
	return fmt.Sprintf("/graalvm/%d/latest/graalvm-jdk-%d_%s-%s_bin%s", b.Version.Major, b.Version.Major, b.Platform.OS, b.Platform.Arch, b.Platform.ArchiveExt())
}

func graalVMArchivePath(b Build) string {
	v := openjdkVersion(b.Version)
	return fmt.Sprintf("/graalvm/%d/archive/graalvm-jdk-%s_%s-%s_bin%s", b.Version.Major, v, b.Platform.OS, b.Platform.Arch, b.Platform.ArchiveExt())
}
