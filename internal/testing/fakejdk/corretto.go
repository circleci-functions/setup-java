package fakejdk

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/circleci-functions/setup-java/javadist"
	"github.com/circleci-functions/setup-java/javaversion"
)

const correttoIndex = "/corretto-downloads/latest_links/indexmap_with_checksum.json"

// serveCorretto lists the newest Corretto build of each major, as Amazon
// does, and serves archives under the version-named paths its index uses.
func serveCorretto(s *Server, w http.ResponseWriter, r *http.Request) bool {
	switch {
	case r.URL.Path == correttoIndex:
	case strings.HasPrefix(r.URL.Path, "/downloads/resources/"):
		for _, b := range s.of("corretto") {
			if correttoResource(b) == r.URL.Path {
				_, _ = w.Write(Archive(b))
				return true
			}
		}
		http.NotFound(w, r)
		return true
	default:
		return false
	}

	index := map[string]map[string]map[string]map[string]map[string]map[string]string{}
	newest := map[string]Build{}
	for _, b := range s.of("corretto") {
		key := fmt.Sprintf("%s/%s/%s/%d", correttoOS(b.Platform), correttoArch(b.Platform.Arch), b.Package, b.Version.Major)
		if seen, ok := newest[key]; !ok || b.Version.Compare(seen.Version) > 0 {
			newest[key] = b
		}
	}
	for _, b := range newest {
		osName, arch := correttoOS(b.Platform), correttoArch(b.Platform.Arch)
		if index[osName] == nil {
			index[osName] = map[string]map[string]map[string]map[string]map[string]string{}
		}
		if index[osName][arch] == nil {
			index[osName][arch] = map[string]map[string]map[string]map[string]string{}
		}
		if index[osName][arch][b.Package] == nil {
			index[osName][arch][b.Package] = map[string]map[string]map[string]string{}
		}
		index[osName][arch][b.Package][strconv.Itoa(b.Version.Major)] = map[string]map[string]string{
			strings.TrimPrefix(b.Platform.ArchiveExt(), "."): {"checksum_sha256": Checksum(b), "resource": correttoResource(b)},
		}
	}
	out := map[string]any{"jmc": map[string]any{"linux": map[string]any{"x64": map[string]any{"tar.gz": map[string]string{"checksum": "x", "resource": "/jmc.tar.gz"}}}}}
	for osName, v := range index {
		out[osName] = v
	}
	writeJSON(w, out)
	return true
}

func correttoResource(b Build) string {
	v := correttoVersion(b.Version)
	return fmt.Sprintf("/downloads/resources/%s/amazon-corretto-%s-%s-%s%s", v, v, correttoOS(b.Platform), b.Platform.Arch, b.Platform.ArchiveExt())
}

func correttoVersion(v javaversion.Version) string {
	if v.Major == 8 {
		return fmt.Sprintf("8.%d.%02d.%d", v.Security, v.Build, v.Revision)
	}
	return fmt.Sprintf("%d.%d.%d.%d.%d", v.Major, v.Minor, v.Security, v.Build, v.Revision)
}

func correttoOS(p javadist.Platform) string {
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
