package javadist

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/circleci-functions/toolkit/archive"
)

// extract unpacks the .tar.gz, .tgz or .zip archive at src under dest, going
// by name for the format.
func extract(src, name, dest string) error {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(src, dest)
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(src, dest)
	}
	return fmt.Errorf("%s: unsupported archive format, want .tar.gz, .tgz or .zip", name)
}

func extractTarGz(src, dest string) error {
	f, err := os.Open(src) //nolint:gosec // src is an archive the caller chose
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := extractTarEntry(tr, hdr, dest); err != nil {
			return err
		}
	}
}

func extractTarEntry(tr *tar.Reader, hdr *tar.Header, dest string) error {
	target, err := within(dest, hdr.Name)
	if err != nil {
		return err
	}
	mode := hdr.FileInfo().Mode().Perm()

	switch hdr.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(target, mode|0700)
	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
			return err
		}
		return archive.WriteFile(target, tr, mode|0600) //nolint:gosec // decompression size is bounded by the archive
	case tar.TypeSymlink:
		//nolint:gosec // this is the traversal check for the link's target
		if _, err := within(dest, filepath.Join(filepath.Dir(hdr.Name), hdr.Linkname)); err != nil || filepath.IsAbs(hdr.Linkname) {
			return fmt.Errorf("symlink %s points outside the archive", hdr.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
			return err
		}
		return os.Symlink(hdr.Linkname, target)
	case tar.TypeLink:
		source, err := within(dest, hdr.Linkname)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
			return err
		}
		return os.Link(source, target)
	}
	return nil
}

func extractZip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()

	for _, f := range r.File {
		target, err := within(dest, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0750); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
			return err
		}
		if err := extractZipFile(f, target); err != nil {
			return err
		}
	}
	return nil
}

func extractZipFile(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	return archive.WriteFile(target, rc, f.Mode().Perm()|0600) //nolint:gosec // decompression size is bounded by the archive
}

// within joins name onto dest, rejecting any name that would land outside it.
func within(dest, name string) (string, error) {
	target := filepath.Join(dest, filepath.FromSlash(name))
	if target != filepath.Clean(dest) && !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry %q escapes the destination", name)
	}
	return target, nil
}
