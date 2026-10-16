package fakejdk

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/circleci-functions/setup-java/javaversion"
)

const openjdkDownloads = "/java/GA/"

// serveJDKJavaNet serves jdk.java.net's pages: a home page linking a page per
// major, that page linking the newest build of the major, and an archive page
// linking every build. Downloads sit under the paths Oracle uses, each with a
// .sha256 file beside it.
func serveJDKJavaNet(s *Server, w http.ResponseWriter, r *http.Request) bool {
	builds := s.of("oracle-openjdk")
	slices.SortFunc(builds, func(a, b Build) int { return b.Version.Compare(a.Version) })
	path := r.URL.Path

	switch {
	case path == "/":
		var majors []int
		for _, b := range builds {
			if !slices.Contains(majors, b.Version.Major) {
				majors = append(majors, b.Version.Major)
				_, _ = fmt.Fprintf(w, "<a href=\"/%d/\">JDK %d</a>\n", b.Version.Major, b.Version.Major)
			}
		}
	case path == "/archive/":
		for _, b := range builds {
			writeOpenJDKLinks(w, s, b)
		}
	case strings.HasPrefix(path, openjdkDownloads):
		for _, b := range builds {
			link := openjdkPath(b)
			switch path {
			case link:
				_, _ = w.Write(Archive(b))
				return true
			case link + ".sha256":
				_, _ = fmt.Fprintln(w, Checksum(b))
				return true
			}
		}
		http.NotFound(w, r)
	default:
		major, err := strconv.Atoi(strings.Trim(path, "/"))
		if err != nil {
			return false
		}
		for _, b := range builds {
			if b.Version.Major == major {
				writeOpenJDKLinks(w, s, b)
				break
			}
		}
	}
	return true
}

func writeOpenJDKLinks(w http.ResponseWriter, s *Server, b Build) {
	_, _ = fmt.Fprintf(w, "<a href=\"%s%s\">tar.gz</a> <a href=\"%s%s.sha256\">sha256</a>\n", s.URL, openjdkPath(b), s.URL, openjdkPath(b))
}

func openjdkPath(b Build) string {
	v := openjdkVersion(b.Version)
	return fmt.Sprintf("%sjdk%s/0123abcd/%d/GPL/openjdk-%s_%s-%s_bin%s", openjdkDownloads, v, b.Version.Build, v, b.Platform.OS, b.Platform.Arch, b.Platform.ArchiveExt())
}

// openjdkVersion writes v as jdk.java.net's file names do: 21 for 21.0.0.
func openjdkVersion(v javaversion.Version) string {
	if v.Minor == 0 && v.Security == 0 && v.Patch == 0 {
		return strconv.Itoa(v.Major)
	}
	if v.Patch > 0 {
		return fmt.Sprintf("%d.%d.%d.%d", v.Major, v.Minor, v.Security, v.Patch)
	}
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Security)
}
