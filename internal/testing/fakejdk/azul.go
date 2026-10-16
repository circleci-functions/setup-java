package fakejdk

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/circleci-functions/setup-java/javadist"
)

const azulPackages = "/metadata/v1/zulu/packages/"

func serveAzul(s *Server, w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, azulPackages) {
		return false
	}
	if uuid := strings.TrimPrefix(r.URL.Path, azulPackages); uuid != "" {
		s.serveAzulPackage(w, r, uuid)
		return true
	}

	q := r.URL.Query()
	var matched []Build
	for _, b := range s.of("zulu") {
		if azulMatches(b, q) {
			matched = append(matched, b)
		}
	}
	slices.SortFunc(matched, func(a, b Build) int { return b.Version.Compare(a.Version) })
	if q.Get("latest") == "true" {
		matched = slices.CompactFunc(matched, func(a, b Build) bool { return a.Version.Major == b.Version.Major })
	}

	size, _ := strconv.Atoi(q.Get("page_size"))
	page, _ := strconv.Atoi(q.Get("page"))
	start := (page - 1) * size
	if size <= 0 || page < 1 || (start >= len(matched) && page > 1) {
		http.Error(w, `{"code":404,"status":"Not Found"}`, http.StatusNotFound)
		return true
	}
	matched = matched[min(start, len(matched)):min(start+size, len(matched))]

	out := make([]map[string]any, 0, len(matched))
	for _, b := range matched {
		v := b.Version
		out = append(out, map[string]any{
			"download_url":         s.downloadURL(b),
			"java_version":         []int{v.Major, v.Minor, v.Security, v.Patch},
			"distro_version":       []int{v.Major, 1, 0, 0},
			"openjdk_build_number": v.Build,
			"package_uuid":         azulUUID(b),
		})
	}
	writeJSON(w, out)
	return true
}

func (s *Server) serveAzulPackage(w http.ResponseWriter, r *http.Request, uuid string) {
	for _, b := range s.of("zulu") {
		if azulUUID(b) == uuid {
			writeJSON(w, map[string]any{"sha256_hash": Checksum(b)})
			return
		}
	}
	http.NotFound(w, r)
}

// azulMatches reports whether b is one of the packages q asks for.
func azulMatches(b Build, q url.Values) bool {
	kind, extra, _ := strings.Cut(b.Package, "+")
	return azulOS(b.Platform) == q.Get("os") &&
		azulArch(b.Platform.Arch) == q.Get("arch") &&
		"."+q.Get("archive_type") == b.Platform.ArchiveExt() &&
		kind == q.Get("java_package_type") &&
		q.Get("javafx_bundled") == strconv.FormatBool(extra == "fx") &&
		q.Get("crac_supported") == strconv.FormatBool(extra == "crac") &&
		(q.Get("java_version") == "" || q.Get("java_version") == strconv.Itoa(b.Version.Major))
}

func azulUUID(b Build) string {
	sum := sha256.Sum256([]byte(ArchiveName(b)))
	return hex.EncodeToString(sum[:16])
}

func azulOS(p javadist.Platform) string {
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
