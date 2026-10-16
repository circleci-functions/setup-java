package javadist

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	hc "github.com/circleci-functions/toolkit/httpcl"

	"github.com/circleci-functions/setup-java/javaversion"
)

// macHome is where a macOS JDK bundle keeps the JDK itself.
var macHome = filepath.Join("Contents", "Home")

// Home returns the JAVA_HOME of the JDK installed at root: root itself, or
// its Contents/Home for a macOS bundle.
func Home(root string) string {
	if bundle := filepath.Join(root, macHome); exists(Java(bundle)) {
		return bundle
	}
	return root
}

// Java returns the java executable of the JDK whose JAVA_HOME is home.
func Java(home string) string {
	name := "java"
	if runtime.GOOS == "windows" {
		name = "java.exe"
	}
	return filepath.Join(home, "bin", name)
}

func Installed(root string) bool {
	return exists(Java(Home(root)))
}

// ReleaseVersion reads the version of the JDK whose JAVA_HOME is home from
// its release file.
func ReleaseVersion(home string) (javaversion.Version, error) {
	data, err := os.ReadFile(filepath.Join(home, "release")) //nolint:gosec // a file in a JDK this package installed
	if err != nil {
		return javaversion.Version{}, err
	}
	props := map[string]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			props[k] = strings.Trim(v, `"`)
		}
	}
	for _, key := range []string{"JAVA_RUNTIME_VERSION", "JAVA_VERSION"} {
		if v, err := javaversion.ParseVersion(props[key]); err == nil && props[key] != "" {
			return v, nil
		}
	}
	return javaversion.Version{}, fmt.Errorf("no Java version in %s", filepath.Join(home, "release"))
}

// Install downloads r, checks it against its SHA-256 when the vendor
// publishes one, and unpacks it so that dest is the root of the JDK. Anything
// already at dest, such as an incomplete install, is replaced.
func Install(ctx context.Context, r Release, dest string) error {
	name := path.Base(r.URL)
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0750); err != nil {
		return err
	}
	archive, err := os.CreateTemp(parent, name+".*.download")
	if err != nil {
		return err
	}
	defer func() {
		_ = archive.Close()
		_ = os.Remove(archive.Name())
	}()

	if err := download(ctx, r.URL, archive); err != nil {
		return err
	}
	if r.SHA256 != "" {
		if err := verify(archive, r.SHA256); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if err := archive.Close(); err != nil {
		return err
	}
	return InstallArchive(archive.Name(), name, dest)
}

// InstallArchive unpacks the JDK archive at src, whose file name is name, so
// that dest is the root of the JDK. The archive may hold the JDK at its top
// level or inside a single directory, as vendors publish it.
func InstallArchive(src, name, dest string) error {
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0750); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, filepath.Base(dest)+".*.extract")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }()

	if err := extract(src, name, staging); err != nil {
		return fmt.Errorf("extract %s: %w", name, err)
	}
	root, err := jdkRoot(staging)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	return os.Rename(root, dest)
}

// jdkRoot finds the JDK in an unpacked archive: dir itself, or the single
// directory inside it.
func jdkRoot(dir string) (string, error) {
	if Installed(dir) {
		return dir, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) == 1 && Installed(filepath.Join(dir, dirs[0])) {
		return filepath.Join(dir, dirs[0]), nil
	}
	return "", fmt.Errorf("no JDK found in the archive: want bin/%s at its top level or in its one directory", filepath.Base(Java("")))
}

// download writes the file at url to f, starting over on each retry.
func download(ctx context.Context, url string, f *os.File) error {
	client := hc.New(ctx, hc.Config{Name: "jdk-download", Timeout: 20 * time.Minute})
	err := client.Call(ctx, hc.NewRequest(http.MethodGet, "%s",
		hc.RouteParams(url),
		hc.SuccessDecoder(func(r io.Reader) error {
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return err
			}
			if err := f.Truncate(0); err != nil {
				return err
			}
			_, err := io.Copy(f, r)
			return err
		}),
		hc.Timeout(15*time.Minute),
	))
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	return nil
}

func verify(f *os.File, want string) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != strings.ToLower(want) {
		return fmt.Errorf("checksum mismatch: got sha256 %s, want %s", got, want)
	}
	return nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
