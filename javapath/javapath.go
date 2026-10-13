// Package javapath makes an installed JDK the one the rest of a CircleCI job
// runs: its tools linked onto PATH and JAVA_HOME exported to later steps.
package javapath

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/circleci-functions/toolkit/bashenv"
	"github.com/circleci-functions/toolkit/pathlink"
)

type Options struct {
	Home string
	// BinDir links into exactly this directory, skipping PATH discovery.
	BinDir string
	// Path is the search path to use. Empty means this process's PATH.
	Path string
}

// Link makes java, javac and every other tool in the JDK's bin directory
// resolvable by name from a directory already on PATH, and returns that
// directory.
func Link(opts Options) (string, error) {
	from := filepath.Join(opts.Home, "bin")
	names, err := Tools(from)
	if err != nil {
		return "", err
	}
	res, err := pathlink.Ensure(pathlink.Options{
		Names:     names,
		FromDir:   from,
		Preferred: pathlink.UserBinDirs(),
		Dir:       opts.BinDir,
		Path:      opts.Path,
		Verify:    verify(opts.Home),
	})
	if err != nil {
		return "", err
	}
	return res.Dir, nil
}

// Tools returns the names of the executables in dir, java first, without the
// .exe suffix they carry on Windows.
func Tools(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if name, ok := tool(e); ok && name != "java" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return append([]string{"java"}, names...), nil
}

func tool(e os.DirEntry) (string, bool) {
	if e.IsDir() {
		return "", false
	}
	if runtime.GOOS == "windows" {
		return strings.CutSuffix(e.Name(), ".exe")
	}
	info, err := e.Info()
	if err != nil {
		return "", false
	}
	return e.Name(), info.Mode().Perm()&0111 != 0
}

// verify checks that the java PATH resolves runs the JDK at home.
func verify(home string) func(bin string) error {
	return func(bin string) error {
		got, _, err := Probe(bin)
		if err != nil {
			return err
		}
		if !sameDir(got, home) && !sameDir(got, filepath.Join(home, "jre")) {
			return fmt.Errorf("%s runs the JDK at %s, not %s", bin, got, home)
		}
		return nil
	}
}

// Probe runs the java at bin and returns the java.home and java.version it
// reports.
func Probe(bin string) (home, version string, err error) {
	out, err := pathlink.Command(bin, "-XshowSettings:properties", "-version").CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("%s does not run: %w: %s", bin, err, strings.TrimSpace(string(out)))
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), " = ")
		switch {
		case !ok:
		case key == "java.home":
			home = value
		case key == "java.version":
			version = value
		}
	}
	if home == "" {
		return "", "", fmt.Errorf("%s did not report java.home: %s", bin, strings.TrimSpace(string(out)))
	}
	return home, version, nil
}

func sameDir(a, b string) bool {
	a, b = realPath(a), realPath(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// HomeVar names the variable that holds the JAVA_HOME of a JDK by major
// version and architecture, e.g. JAVA_HOME_21_X64, which build-tool
// toolchains look JDKs up by.
func HomeVar(major int, arch string) string {
	return fmt.Sprintf("JAVA_HOME_%d_%s", major, strings.ToUpper(arch))
}

// Export appends vars to $BASH_ENV for later steps. Values are quoted for the
// shell, and on Windows written with forward slashes, which both Git Bash and
// the JDK read as a Windows path.
func Export(vars map[string]string) error {
	if os.Getenv("BASH_ENV") == "" {
		return errors.New("BASH_ENV is not set, so JAVA_HOME cannot be exported to later steps")
	}
	quoted := make(map[string]string, len(vars))
	for k, v := range vars {
		if runtime.GOOS == "windows" {
			v = filepath.ToSlash(v)
		}
		quoted[k] = "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
	}
	return bashenv.ExportEnvVars(quoted)
}
