package fakejdk

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/circleci-functions/setup-java/javaversion"
)

// ArchiveName is the file name the server gives b's archive.
func ArchiveName(b Build) string {
	return fmt.Sprintf("%s-%s_%s_%s%s", b.Distribution, b.Package, b.Platform, b.Version.DirName(), b.Platform.ArchiveExt())
}

// Checksum is the hex SHA-256 of b's archive.
func Checksum(b Build) string {
	sum := sha256.Sum256(Archive(b))
	return hex.EncodeToString(sum[:])
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
	files := map[string]string{
		"release": fmt.Sprintf("JAVA_VERSION=%q\nJAVA_RUNTIME_VERSION=%q\n", short(b.Version), b.Version.String()),
	}
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
echo 'OpenJDK Runtime Environment (build ` + v.String() + `)' >&2
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
