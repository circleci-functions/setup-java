package javadist_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/setup-java/internal/testing/fakeadoptium"
	"github.com/circleci-functions/setup-java/javadist"
)

func TestInstall(t *testing.T) {
	b := fakeadoptium.Build{Version: ver(21, 6, 7)}
	srv := fakeadoptium.New(t, b)
	r, err := resolve(t, srv, "21", host(t), "jdk")
	assert.NilError(t, err)

	dest := filepath.Join(t.TempDir(), "jdk")
	assert.NilError(t, os.MkdirAll(filepath.Join(dest, "stale"), 0750))
	assert.NilError(t, javadist.Install(context.Background(), r, dest))

	assert.Assert(t, javadist.Installed(dest))
	_, err = os.Stat(filepath.Join(dest, "stale"))
	assert.Assert(t, os.IsNotExist(err), "an earlier install at dest is replaced")
	entries, err := os.ReadDir(filepath.Dir(dest))
	assert.NilError(t, err)
	assert.Equal(t, len(entries), 1, "the download and staging directory are cleaned up: %v", entries)
}

func TestInstall_ChecksumMismatch(t *testing.T) {
	srv := fakeadoptium.New(t, fakeadoptium.Build{Version: ver(21, 6, 7)})
	r, err := resolve(t, srv, "21", host(t), "jdk")
	assert.NilError(t, err)
	srv.Corrupt()

	dest := filepath.Join(t.TempDir(), "jdk")
	err = javadist.Install(context.Background(), r, dest)
	assert.ErrorContains(t, err, "checksum mismatch")
	assert.Assert(t, !javadist.Installed(dest))
}

func TestInstall_NoChecksum(t *testing.T) {
	srv := fakeadoptium.New(t, fakeadoptium.Build{Version: ver(21, 6, 7)})
	r, err := resolve(t, srv, "21", host(t), "jdk")
	assert.NilError(t, err)
	srv.Corrupt()
	r.SHA256 = ""

	dest := filepath.Join(t.TempDir(), "jdk")
	assert.NilError(t, javadist.Install(context.Background(), r, dest), "with no checksum published there is nothing to check against")
	assert.Assert(t, javadist.Installed(dest))
}

func TestInstallArchive_Layouts(t *testing.T) {
	java := filepath.Base(javadist.Java(""))
	tests := []struct {
		name  string
		files []string
		home  string
	}{
		{name: "in one directory", files: []string{"jdk-21/bin/" + java, "jdk-21/release"}},
		{name: "at the top level", files: []string{"bin/" + java, "release"}},
		{name: "macOS bundle", files: []string{"jdk-21.jdk/Contents/Home/bin/" + java, "jdk-21.jdk/Contents/Info.plist"}, home: filepath.Join("Contents", "Home")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := writeTarGz(t, tc.files)
			dest := filepath.Join(t.TempDir(), "jdk")
			assert.NilError(t, javadist.InstallArchive(src, "jdk.tar.gz", dest))
			assert.Equal(t, javadist.Home(dest), filepath.Join(dest, tc.home))
			assert.Assert(t, javadist.Installed(dest))
		})
	}
}

func TestInstallArchive_Rejects(t *testing.T) {
	java := filepath.Base(javadist.Java(""))
	tests := []struct {
		name  string
		files []string
		file  string
		want  string
	}{
		{name: "no JDK", files: []string{"docs/readme.txt"}, file: "jdk.tar.gz", want: "no JDK found in the archive"},
		{name: "two directories", files: []string{"a/bin/" + java, "b/bin/" + java}, file: "jdk.tar.gz", want: "no JDK found in the archive"},
		{name: "escapes the destination", files: []string{"../evil/bin/" + java}, file: "jdk.tar.gz", want: "escapes the destination"},
		{name: "unsupported format", files: []string{"bin/" + java}, file: "jdk.rar", want: "unsupported archive format"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := writeTarGz(t, tc.files)
			err := javadist.InstallArchive(src, tc.file, filepath.Join(t.TempDir(), "jdk"))
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func writeTarGz(t *testing.T, files []string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range files {
		assert.NilError(t, tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0755, Size: 1}))
		_, err := tw.Write([]byte("x"))
		assert.NilError(t, err)
	}
	assert.NilError(t, tw.Close())
	assert.NilError(t, gz.Close())
	path := filepath.Join(t.TempDir(), "archive")
	assert.NilError(t, os.WriteFile(path, buf.Bytes(), 0600))
	return path
}
