// Package fakejdk serves stand-ins for the vendor APIs setup-java looks JDK
// builds up in, and the archives they list, so tests can resolve, download
// and unpack JDKs without the network. One server answers for every vendor,
// each under the paths its real API uses.
package fakejdk

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/circleci-functions/setup-java/javadist"
	"github.com/circleci-functions/setup-java/javaversion"
)

// Build is one JDK build the server lists.
type Build struct {
	// Distribution defaults to temurin.
	Distribution string
	Version      javaversion.Version
	// Package defaults to jdk.
	Package string
	// Platform defaults to the host.
	Platform javadist.Platform
}

// Server is a running fake of every vendor API.
type Server struct {
	URL string

	builds   []Build
	mu       sync.Mutex
	requests []string
	corrupt  bool
}

// vendors answer the requests under their API's paths, reporting false for a
// request that is not theirs.
var vendors = []func(s *Server, w http.ResponseWriter, r *http.Request) bool{
	serveAdoptium,
	serveAzul,
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
		if b.Distribution == "" {
			b.Distribution = "temurin"
		}
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

// of returns the builds of distribution.
func (s *Server) of(distribution string) []Build {
	var out []Build
	for _, b := range s.builds {
		if b.Distribution == distribution {
			out = append(out, b)
		}
	}
	return out
}

// downloadURL is where the server serves b's archive.
func (s *Server) downloadURL(b Build) string {
	return s.URL + "/download/" + ArchiveName(b)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, r.URL.RequestURI())
	s.mu.Unlock()

	if strings.HasPrefix(r.URL.Path, "/download/") {
		s.serveArchive(w, r)
		return
	}
	for _, vendor := range vendors {
		if vendor(s, w, r) {
			return
		}
	}
	http.NotFound(w, r)
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
