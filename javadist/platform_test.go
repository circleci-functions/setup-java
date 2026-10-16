package javadist

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestPlatformFor(t *testing.T) {
	tests := []struct {
		name   string
		goos   string
		goarch string
		arch   string
		musl   bool
		want   Platform
	}{
		{name: "linux glibc", goos: "linux", goarch: "amd64", want: Platform{OS: "linux", Arch: "x64", Libc: "glibc"}},
		{name: "linux musl", goos: "linux", goarch: "arm64", musl: true, want: Platform{OS: "linux", Arch: "aarch64", Libc: "musl"}},
		{name: "macos", goos: "darwin", goarch: "arm64", want: Platform{OS: "macos", Arch: "aarch64"}},
		{name: "windows", goos: "windows", goarch: "amd64", want: Platform{OS: "windows", Arch: "x64"}},
		{name: "explicit arch", goos: "windows", goarch: "amd64", arch: "x86", want: Platform{OS: "windows", Arch: "x86"}},
		{name: "explicit alias", goos: "darwin", goarch: "arm64", arch: "AMD64", want: Platform{OS: "macos", Arch: "x64"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := platformFor(tc.goos, tc.goarch, tc.arch, tc.musl)
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestPlatformFor_Unsupported(t *testing.T) {
	_, err := platformFor("plan9", "amd64", "", false)
	assert.ErrorContains(t, err, "unsupported operating system")
	_, err = platformFor("linux", "mips", "", false)
	assert.ErrorContains(t, err, `unsupported architecture "mips"`)
	_, err = platformFor("linux", "amd64", "sparc", false)
	assert.ErrorContains(t, err, `unsupported architecture "sparc"`)
}

func TestNormalizeArch(t *testing.T) {
	tests := map[string]string{
		"amd64": "x64", "x64": "x64", "arm64": "aarch64", "aarch64": "aarch64",
		"ia32": "x86", "386": "x86", "arm": "armv7", " PPC64LE ": "ppc64le", "riscv64": "riscv64",
	}
	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			got, err := NormalizeArch(in)
			assert.NilError(t, err)
			assert.Equal(t, got, want)
		})
	}
}

func TestPlatform_String(t *testing.T) {
	assert.Equal(t, Platform{OS: "linux", Arch: "x64", Libc: "musl"}.String(), "linux-x64-musl")
	assert.Equal(t, Platform{OS: "macos", Arch: "aarch64"}.String(), "macos-aarch64")
	assert.Equal(t, Platform{OS: "windows", Arch: "x64"}.ArchiveExt(), ".zip")
	assert.Equal(t, Platform{OS: "linux", Arch: "x64", Libc: "glibc"}.ArchiveExt(), ".tar.gz")
}
