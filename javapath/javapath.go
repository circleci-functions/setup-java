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

// Result describes where Link made the JDK resolvable from.
type Result struct {
	// Dir is the directory the JDK tools were linked into, or "" when they
	// were not linked.
	Dir string
	// Shadowed is the directory of another JDK's java that comes first on
	// PATH, when Link left it alone rather than replace it. The caller puts
	// the JDK on PATH some other way.
	Shadowed string
}

// Link makes java, javac and every other tool in the JDK's bin directory
// resolvable by name from a directory already on PATH.
//
// On Windows it will not replace another JDK's tools to get ahead of them: a
// JDK's java.exe cannot be swapped for a shim without breaking that JDK. When
// no directory comes ahead of it, Link links nothing and reports it in
// Result.Shadowed.
func Link(opts Options) (Result, error) {
	from := filepath.Join(opts.Home, "bin")
	names, err := Tools(from)
	if err != nil {
		return Result{}, err
	}
	if opts.BinDir == "" && runtime.GOOS == "windows" {
		if dir, ok := takesOver(pathlink.SearchDirs(opts.Path), pathlink.UserBinDirs(), pathlink.Writable); ok {
			return Result{Shadowed: dir}, nil
		}
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
		return Result{}, err
	}
	return Result{Dir: res.Dir}, nil
}

// takesOver returns the directory of the java already on PATH when linking
// would replace it there, because no directory in preferred comes ahead of
// it. A java in one of preferred is a link from an earlier run, safe to
// replace.
func takesOver(dirs, preferred []string, writable func(string) bool) (string, bool) {
	existing := pathlink.IndexOf(dirs, "java")
	if existing < 0 {
		return "", false
	}
	dir, _ := pathlink.ChooseDir(dirs, existing, preferred, writable)
	if dir != dirs[existing] || slices.ContainsFunc(preferred, func(p string) bool { return sameDir(p, dir) }) {
		return "", false
	}
	return dir, true
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

// PrependPath puts dir first on PATH in later steps, through $BASH_ENV, so
// only steps that run in bash see it. On Windows the path is written as Git
// Bash spells it, /c/Users/..., since PATH there is colon-separated.
func PrependPath(dir string) error {
	if os.Getenv("BASH_ENV") == "" {
		return errors.New("BASH_ENV is not set, so PATH cannot be changed for later steps")
	}
	return bashenv.AddToPath(shellQuote(unixPath(dir)))
}

// unixPath turns a Windows path like C:\Users\x into /c/Users/x. Other paths
// come back with forward slashes and are otherwise unchanged.
func unixPath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if len(p) >= 2 && p[1] == ':' {
		return "/" + strings.ToLower(p[:1]) + p[2:]
	}
	return p
}

func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

// Export appends vars to $BASH_ENV for later steps. Values are quoted for the
// shell, and on Windows written with forward slashes, which both Git Bash and
// the JDK read as a Windows path.
func Export(vars map[string]string) error {
	if os.Getenv("BASH_ENV") == "" {
		return errors.New("BASH_ENV is not set, so JAVA_HOME cannot be exported to later steps")
	}
	values := make(map[string]string, len(vars))
	for k, v := range vars {
		if runtime.GOOS == "windows" {
			v = filepath.ToSlash(v)
		}
		values[k] = v
	}
	return bashenv.ExportEnvVars(values)
}
