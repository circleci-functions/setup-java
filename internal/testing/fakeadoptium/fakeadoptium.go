// Package fakeadoptium serves a stand-in for the Adoptium API and the Temurin
// archives it lists, so tests can resolve, download and unpack JDKs without
// the network.
package fakeadoptium

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/circleci-functions/setup-java/javadist"
	"github.com/circleci-functions/setup-java/javaversion"
)

// Build is one Temurin build the server lists.
type Build struct {
	Version javaversion.Version
	// Package defaults to jdk.
	Package string
	// Platform defaults to the host.
	Platform javadist.Platform
}

// Server is a running fake Adoptium API.
type Server struct {
	URL string

	builds   []Build
	mu       sync.Mutex
	requests []string
	corrupt  bool
}

// New starts a server listing builds, and stops it when t ends.
func New(t *testing.T, builds ...Build) *Server {
	t.Helper()
	host, err := javadist.Host("")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	for _, b := range builds {
		if b.Package == "" {
			b.Package = "jdk"
		}
		if b.Platform == (javadist.Platform{}) {
			b.Platform = host
		}
		s.builds = append(s.builds, b)
	}
	srv := httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(srv.Close)
	s.URL = srv.URL
	return s
}

// Corrupt makes the server send archives that do not match their checksums.
func (s *Server) Corrupt() {
	s.corrupt = true
}

// Requests returns the paths requested so far, with their queries.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

// Downloads returns how many archives have been served.
func (s *Server) Downloads() int {
	n := 0
	for _, r := range s.Requests() {
		if strings.HasPrefix(r, "/download/") {
			n++
		}
	}
	return n
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, r.URL.RequestURI())
	s.mu.Unlock()

	switch {
	case r.URL.Path == "/v3/info/available_releases":
		s.serveInfo(w)
	case strings.HasPrefix(r.URL.Path, "/v3/assets/version/"):
		s.serveAssets(w, r)
	case strings.HasPrefix(r.URL.Path, "/download/"):
		s.serveArchive(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) serveInfo(w http.ResponseWriter) {
	latest := 0
	for _, b := range s.builds {
		if !b.Version.EA {
			latest = max(latest, b.Version.Major)
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]int{"most_recent_feature_release": latest})
}

func (s *Server) serveAssets(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	low, high, ok := parseRange(strings.TrimPrefix(r.URL.Path, "/v3/assets/version/"))
	if !ok {
		http.Error(w, "bad range", http.StatusBadRequest)
		return
	}
	var matched []Build
	for _, b := range s.builds {
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
		assets = append(assets, s.asset(b))
	}
	_ = json.NewEncoder(w).Encode(assets)
}

func (s *Server) asset(b Build) map[string]any {
	sum := sha256.Sum256(Archive(b))
	v := b.Version
	data := map[string]any{
		"major": v.Major, "minor": v.Minor, "security": v.Security, "patch": v.Patch, "build": v.Build,
	}
	if v.EA {
		data["optional"] = "ea"
		data["pre"] = "beta"
	}
	return map[string]any{
		"binaries": []map[string]any{{
			"package": map[string]any{
				"checksum": hex.EncodeToString(sum[:]),
				"link":     s.URL + "/download/" + ArchiveName(b),
				"name":     ArchiveName(b),
			},
		}},
		"version_data": data,
	}
}

func (s *Server) serveArchive(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/download/")
	for _, b := range s.builds {
		if ArchiveName(b) != name {
			continue
		}
		data := Archive(b)
		if s.corrupt {
			data = append(data, 0)
		}
		_, _ = w.Write(data)
		return
	}
	http.NotFound(w, r)
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

// ArchiveName is the file name the server gives b's archive.
func ArchiveName(b Build) string {
	return fmt.Sprintf("OpenJDK-%s_%s_%s%s", b.Package, b.Platform, b.Version.DirName(), b.Platform.ArchiveExt())
}

// Archive returns b's archive, laid out like a real one under a top-level
// jdk-<version> directory, inside Contents/Home on macOS. On Unix, bin/java,
// bin/javac and bin/jar are shell scripts that report the version and, for
// java -XshowSettings:properties, java.home, so a test can run them.
func Archive(b Build) []byte {
	top := "jdk-" + b.Version.String() + "/"
	if b.Platform.OS == "macos" {
		top += "Contents/Home/"
	}
	files := map[string]string{"release": fmt.Sprintf("JAVA_VERSION=%q\n", short(b.Version))}
	if b.Platform.OS == "windows" {
		files["bin/java.exe"] = "fake java " + b.Version.String()
		files["bin/javac.exe"] = "fake javac " + b.Version.String()
		return zipArchive(top, files)
	}
	files["bin/java"] = JavaScript(b.Version)
	files["bin/javac"] = "#!/bin/sh\necho 'javac " + short(b.Version) + "'\n"
	files["bin/jar"] = "#!/bin/sh\nexit 0\n"
	return tarGzArchive(top, files)
}

// JavaScript is a shell script that answers like the java of v, following
// symlinks to find the JDK it is in, as the real java does.
func JavaScript(v javaversion.Version) string {
	return `#!/bin/sh
p="$0"
while [ -L "$p" ]; do
  l=$(readlink "$p")
  case "$l" in /*) p="$l" ;; *) p="$(dirname "$p")/$l" ;; esac
done
home=$(cd "$(dirname "$p")/.." && pwd -P)
for a in "$@"; do
  if [ "$a" = "-XshowSettings:properties" ]; then
    echo "Property settings:" >&2
    echo "    java.home = $home" >&2
    echo "    java.version = ` + short(v) + `" >&2
  fi
done
echo 'openjdk version "` + short(v) + `"' >&2
echo 'OpenJDK Runtime Environment Temurin-` + v.String() + ` (build ` + v.String() + `)' >&2
`
}

func short(v javaversion.Version) string {
	s := v.String()
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	return s
}

func tarGzArchive(top string, files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: top, Typeflag: tar.TypeDir, Mode: 0755})
	_ = tw.WriteHeader(&tar.Header{Name: top + "bin/", Typeflag: tar.TypeDir, Mode: 0755})
	for _, name := range sortedKeys(files) {
		mode := int64(0644)
		if strings.HasPrefix(name, "bin/") {
			mode = 0755
		}
		_ = tw.WriteHeader(&tar.Header{Name: top + name, Typeflag: tar.TypeReg, Mode: mode, Size: int64(len(files[name]))})
		_, _ = tw.Write([]byte(files[name]))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func zipArchive(top string, files map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range sortedKeys(files) {
		f, _ := zw.Create(top + name)
		_, _ = f.Write([]byte(files[name]))
	}
	_ = zw.Close()
	return buf.Bytes()
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
