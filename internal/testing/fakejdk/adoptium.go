package fakejdk

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/circleci-functions/setup-java/javadist"
)

func serveAdoptium(s *Server, w http.ResponseWriter, r *http.Request) bool {
	switch {
	case r.URL.Path == "/v3/info/available_releases":
		latest := 0
		for _, b := range s.of("temurin") {
			if !b.Version.EA {
				latest = max(latest, b.Version.Major)
			}
		}
		writeJSON(w, map[string]int{"most_recent_feature_release": latest})
	case strings.HasPrefix(r.URL.Path, "/v3/assets/version/"):
		s.serveAdoptiumAssets(w, r)
	default:
		return false
	}
	return true
}

func (s *Server) serveAdoptiumAssets(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	low, high, ok := parseRange(strings.TrimPrefix(r.URL.Path, "/v3/assets/version/"))
	if !ok {
		http.Error(w, "bad range", http.StatusBadRequest)
		return
	}
	var matched []Build
	for _, b := range s.of("temurin") {
		if adoptiumOS(b.Platform) == q.Get("os") &&
			adoptiumArch(b.Platform.Arch) == q.Get("architecture") &&
			b.Package == q.Get("image_type") &&
			b.Version.EA == (q.Get("release_type") == "ea") &&
			b.Version.Major >= low && b.Version.Major < high {
			matched = append(matched, b)
		}
	}
	slices.SortFunc(matched, func(a, b Build) int { return b.Version.Compare(a.Version) })

	size, _ := strconv.Atoi(q.Get("page_size"))
	page, _ := strconv.Atoi(q.Get("page"))
	start := page * size
	if size <= 0 || start >= len(matched) {
		http.Error(w, "No releases match the request", http.StatusNotFound)
		return
	}
	matched = matched[start:min(start+size, len(matched))]

	assets := make([]map[string]any, 0, len(matched))
	for _, b := range matched {
		v := b.Version
		data := map[string]any{
			"major": v.Major, "minor": v.Minor, "security": v.Security, "patch": v.Patch, "build": v.Build,
		}
		if v.EA {
			data["optional"] = "ea"
			data["pre"] = "beta"
		}
		assets = append(assets, map[string]any{
			"binaries": []map[string]any{{
				"package": map[string]any{
					"checksum": Checksum(b),
					"link":     s.downloadURL(b),
					"name":     ArchiveName(b),
				},
			}},
			"version_data": data,
		})
	}
	writeJSON(w, assets)
}

// parseRange reads the majors an Adoptium version range covers, e.g.
// [21,22) or [1.0,100.0].
func parseRange(rng string) (low, high int, ok bool) {
	if len(rng) < 5 || rng[0] != '[' {
		return 0, 0, false
	}
	lo, hi, found := strings.Cut(rng[1:len(rng)-1], ",")
	if !found {
		return 0, 0, false
	}
	l, err1 := strconv.ParseFloat(lo, 64)
	h, err2 := strconv.ParseFloat(hi, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	high = int(h)
	if rng[len(rng)-1] == ']' {
		high++
	}
	return int(l), high, true
}

func adoptiumOS(p javadist.Platform) string {
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
