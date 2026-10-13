// Package javadist finds, downloads, verifies and unpacks JDK builds from the
// distributions setup-java supports.
package javadist

import (
	"debug/elf"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

type Platform struct {
	// OS is linux, macos or windows.
	OS string
	// Arch is x64, x86, aarch64, armv7, ppc64le, ppc64, riscv64 or s390x.
	Arch string
	// Libc is glibc or musl on Linux, and "" elsewhere.
	Libc string
}

var Architectures = []string{"x64", "x86", "aarch64", "armv7", "ppc64le", "ppc64", "riscv64", "s390x"}

var archAliases = map[string]string{
	"amd64": "x64",
	"386":   "x86",
	"ia32":  "x86",
	"arm64": "aarch64",
	"arm":   "armv7",
}

var goOS = map[string]string{"linux": "linux", "darwin": "macos", "windows": "windows"}

// Host returns the platform setup-java is running on, for arch when it is
// set and the machine's own architecture otherwise.
func Host(arch string) (Platform, error) {
	return platformFor(runtime.GOOS, runtime.GOARCH, arch, runtime.GOOS == "linux" && isMusl())
}

func platformFor(goos, goarch, arch string, musl bool) (Platform, error) {
	osName, ok := goOS[goos]
	if !ok {
		return Platform{}, fmt.Errorf("unsupported operating system %s", goos)
	}
	if arch == "" {
		arch = goarch
	}
	normalized, err := NormalizeArch(arch)
	if err != nil {
		return Platform{}, err
	}
	p := Platform{OS: osName, Arch: normalized}
	if osName == "linux" {
		p.Libc = "glibc"
		if musl {
			p.Libc = "musl"
		}
	}
	return p, nil
}

// NormalizeArch returns arch in setup-java's names, accepting the common
// aliases amd64, arm64, 386, ia32 and arm.
func NormalizeArch(arch string) (string, error) {
	a := strings.ToLower(strings.TrimSpace(arch))
	if alias, ok := archAliases[a]; ok {
		return alias, nil
	}
	if slices.Contains(Architectures, a) {
		return a, nil
	}
	return "", fmt.Errorf("unsupported architecture %q: use one of %s", arch, strings.Join(Architectures, ", "))
}

// isMusl reports whether this Linux uses musl rather than glibc, as Alpine
// does, by the dynamic loader /bin/sh asks for. The loader merely existing is
// not enough: a glibc distro can install musl alongside.
func isMusl() bool {
	if interp, ok := interpreter("/bin/sh"); ok {
		return strings.Contains(filepath.Base(interp), "musl")
	}
	matches, _ := filepath.Glob("/lib/ld-musl-*.so.1")
	return len(matches) > 0
}

// interpreter returns the dynamic loader the ELF executable at path names.
func interpreter(path string) (string, bool) {
	f, err := elf.Open(path)
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()
	for _, prog := range f.Progs {
		if prog.Type != elf.PT_INTERP {
			continue
		}
		data, err := io.ReadAll(prog.Open())
		if err != nil {
			return "", false
		}
		return strings.TrimRight(string(data), "\x00"), true
	}
	return "", false
}

// String returns the platform as os-arch, with the libc on Linux, e.g.
// linux-x64-musl or macos-aarch64.
func (p Platform) String() string {
	if p.Libc == "" {
		return p.OS + "-" + p.Arch
	}
	return p.OS + "-" + p.Arch + "-" + p.Libc
}

func (p Platform) ArchiveExt() string {
	if p.OS == "windows" {
		return ".zip"
	}
	return ".tar.gz"
}
