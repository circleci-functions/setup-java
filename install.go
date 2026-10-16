package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/circleci-functions/toolkit/sdk"

	"github.com/circleci-functions/setup-java/javadist"
	"github.com/circleci-functions/setup-java/javapath"
	"github.com/circleci-functions/setup-java/javaversion"
)

const jdkFileDistribution = "jdkfile"

// installCmd installs one or more JDKs, makes the last of them the one later
// steps run, and exports each one's home by major version.
type installCmd struct {
	JavaVersion     []string `help:"Java version to install: a major (21), a full version (21.0.5 or 21.0.5+11), a semver range (>=17 <22), or latest. Add -ea for an early-access build (26-ea). Separate several with commas to install each; the last becomes the default." example:"21"`
	JavaVersionFile string   `help:"File to read the Java version from: .java-version, .tool-versions or .sdkmanrc. When --java-version is set too, the two must ask for the same version." type:"path"`
	Distribution    string   `help:"Java distribution to install: temurin (Eclipse Temurin), or jdkfile to install the archive given by --jdk-file." enum:"temurin,jdkfile" required:"" example:"temurin"`
	JavaPackage     string   `help:"Package type to install: jdk or jre." default:"jdk"`
	Architecture    string   `help:"Architecture to install for: x64, x86, aarch64, armv7, ppc64le, ppc64, riscv64 or s390x (amd64 and arm64 work too). Defaults to the machine's."`
	JdkFile         string   `help:"JDK archive to install (.tar.gz, .tgz or .zip), with --distribution jdkfile." type:"path"`
	MakeDefaultJdk  bool     `help:"Make this JDK the one java, javac and JAVA_HOME resolve to. Set to false to install it only for build-tool toolchains, which find it through JAVA_HOME_<major>_<ARCH>." default:"true"`
	Cache           bool     `help:"Restore the JDK from the job cache, and save it after a fresh download. Set to false to disable caching." default:"true"`
	InstallDir      string   `help:"Directory to install JDKs under." default:"~/.cache/setup-java" hidden:""`
	BinDir          string   `help:"Link the JDK tools into this directory instead of discovering one on PATH." type:"path" hidden:""`
	CacheNonce      string   `help:"Partition the JDK cache, so a test run cannot restore an earlier run's install." hidden:""`
	TemurinURL      string   `help:"Adoptium API to look up Temurin builds in." default:"https://api.adoptium.net" hidden:""`
}

type jdk struct {
	label string
	major int
	home  string
}

func (c *installCmd) Run(ctx context.Context, client *sdk.Client) error {
	specs, err := c.specs()
	if err != nil {
		return err
	}
	platform, err := javadist.Host(c.Architecture)
	if err != nil {
		return err
	}
	installDir, err := expandHome(c.InstallDir)
	if err != nil {
		return err
	}

	var jdks []jdk
	for _, spec := range specs {
		j, err := c.install(ctx, client, spec, platform, installDir)
		if err != nil {
			return err
		}
		jdks = append(jdks, j)
	}

	vars := map[string]string{}
	for _, j := range jdks {
		vars[javapath.HomeVar(j.major, platform.Arch)] = j.home
	}
	if c.MakeDefaultJdk {
		def := jdks[len(jdks)-1]
		if err := c.makeDefault(def); err != nil {
			return err
		}
		vars["JAVA_HOME"] = def.home
	} else {
		fmt.Fprintf(os.Stderr, "setup-java: --make-default-jdk=false, so java and JAVA_HOME are left as they were\n")
	}
	export(vars)
	return nil
}

// makeDefault puts j's tools on PATH: linked into a directory already on it,
// or, where that would mean replacing another JDK's tools, first on PATH in
// later bash steps.
func (c *installCmd) makeDefault(j jdk) error {
	res, err := javapath.Link(javapath.Options{Home: j.home, BinDir: c.BinDir})
	if err != nil {
		return fmt.Errorf("put Java %s on PATH: %w", j.label, err)
	}
	if res.Shadowed == "" {
		fmt.Fprintf(os.Stderr, "setup-java: Java %s is the default: java, javac and the other JDK tools are linked into %s\n", j.label, res.Dir)
		return nil
	}
	bin := filepath.Join(j.home, "bin")
	if err := javapath.PrependPath(bin); err != nil {
		return fmt.Errorf("put Java %s on PATH: %w", j.label, err)
	}
	fmt.Fprintf(os.Stderr, "setup-java: Java %s is the default in later bash steps, with %s first on PATH there\n", j.label, bin)
	fmt.Fprintf(os.Stderr, "setup-java: warning: the JDK in %s comes first on PATH and is left as it is, so java outside bash still runs that one\n", res.Shadowed)
	return nil
}

// specs returns the version specs to install, in order, from --java-version
// and --java-version-file.
func (c *installCmd) specs() ([]javaversion.Spec, error) {
	if len(c.JavaVersion) == 0 && c.JavaVersionFile == "" {
		return nil, errors.New("one of --java-version or --java-version-file is required")
	}
	var specs []javaversion.Spec
	for _, raw := range c.JavaVersion {
		spec, err := javaversion.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("--java-version: %w", err)
		}
		specs = append(specs, spec)
	}
	if c.JavaVersionFile != "" {
		spec, err := c.fileSpec()
		if err != nil {
			return nil, err
		}
		switch {
		case len(specs) == 0:
			specs = []javaversion.Spec{spec}
		case !slices.ContainsFunc(specs, func(s javaversion.Spec) bool { return s.String() == spec.String() }):
			return nil, fmt.Errorf("--java-version %s and %s (%s) ask for different versions; they must agree, or set only one of them",
				strings.Join(c.JavaVersion, ","), c.JavaVersionFile, spec)
		}
	}
	return specs, c.checkDistribution(specs)
}

// fileSpec reads the spec from --java-version-file, checking any distribution
// the file names against --distribution.
func (c *installCmd) fileSpec() (javaversion.Spec, error) {
	fv, err := javaversion.FromFile(c.JavaVersionFile)
	if err != nil {
		return javaversion.Spec{}, fmt.Errorf("--java-version-file: %w", err)
	}
	switch {
	case fv.Distribution != "" && fv.Distribution != c.Distribution:
		return javaversion.Spec{}, fmt.Errorf("%s asks for the %s distribution (%s), but --distribution is %s",
			c.JavaVersionFile, fv.Distribution, fv.Vendor, c.Distribution)
	case fv.Distribution == "" && fv.Vendor != "":
		fmt.Fprintf(os.Stderr, "setup-java: %s names a distribution setup-java does not know, %q; installing %s\n", c.JavaVersionFile, fv.Vendor, c.Distribution)
	}
	spec, err := javaversion.Parse(fv.Spec)
	if err != nil {
		return javaversion.Spec{}, fmt.Errorf("--java-version-file: %w", err)
	}
	fmt.Fprintf(os.Stderr, "setup-java: read Java version %q from %s\n", spec, c.JavaVersionFile)
	return spec, nil
}

func (c *installCmd) checkDistribution(specs []javaversion.Spec) error {
	if c.Distribution != jdkFileDistribution {
		if c.JdkFile != "" {
			return errors.New("--jdk-file needs --distribution jdkfile")
		}
		return javadist.CheckPackage(c.distribution(), c.JavaPackage)
	}
	switch {
	case c.JdkFile == "":
		return errors.New("--distribution jdkfile needs --jdk-file, the JDK archive to install")
	case c.JavaPackage != "jdk":
		return fmt.Errorf("--distribution jdkfile installs a jdk package, not %q", c.JavaPackage)
	case len(specs) != 1 || !specs[0].Concrete():
		return errors.New("--distribution jdkfile needs one Java version naming the JDK in --jdk-file, such as 21 or 21.0.5")
	}
	return nil
}

func (c *installCmd) distribution() javadist.Distribution {
	return javadist.Temurin{BaseURL: c.TemurinURL}
}

func (c *installCmd) install(ctx context.Context, client *sdk.Client, spec javaversion.Spec, platform javadist.Platform, installDir string) (jdk, error) {
	var (
		j   jdk
		err error
	)
	if c.Distribution == jdkFileDistribution {
		j, err = c.installFile(spec, platform, installDir)
	} else {
		j, err = c.installRelease(ctx, client, spec, platform, installDir)
	}
	if err != nil {
		return jdk{}, err
	}
	if err := checkRuns(j, platform); err != nil {
		return jdk{}, err
	}
	fmt.Fprintf(os.Stderr, "setup-java: Java %s (%s %s) installed at %s\n", j.label, c.Distribution, c.JavaPackage, j.home)
	return j, nil
}

// installRelease installs the newest release matching spec, restoring it
// from the job cache or downloading it unless an earlier run already did. An
// exact spec is looked up only when there is something to download.
func (c *installCmd) installRelease(ctx context.Context, client *sdk.Client, spec javaversion.Spec, platform javadist.Platform, installDir string) (jdk, error) {
	dist := c.distribution()
	version, exact := spec.Exact()
	var release javadist.Release
	resolve := func() error {
		r, err := dist.Resolve(ctx, spec, platform, c.JavaPackage)
		if err != nil {
			return err
		}
		release = r
		fmt.Fprintf(os.Stderr, "setup-java: resolved %q to %s %s (%s)\n", spec, dist.Name(), r.Version, platform)
		return nil
	}

	if !exact {
		if err := resolve(); err != nil {
			return jdk{}, err
		}
		version = release.Version
	}
	root := filepath.Join(installDir, c.installName(version.DirName(), platform))
	j := jdk{label: version.String(), major: version.Major}
	key := c.cacheKey(version.DirName(), platform, root)
	if c.restore(ctx, client, key, version.String(), root) {
		j.home = javadist.Home(root)
		return j, nil
	}
	if exact {
		if err := resolve(); err != nil {
			return jdk{}, err
		}
	}
	if err := javadist.Install(ctx, release, root); err != nil {
		return jdk{}, err
	}
	if c.Cache {
		sdk.LogCacheErr("save", client.SaveCache(ctx, key, []string{root}, sdk.WithStepName("Saving Java "+version.String())))
	}
	j.home = javadist.Home(root)
	return j, nil
}

// installFile installs the JDK archive given by --jdk-file. It is not cached:
// the archive is already on the machine.
func (c *installCmd) installFile(spec javaversion.Spec, platform javadist.Platform, installDir string) (jdk, error) {
	root := filepath.Join(installDir, c.installName(spec.String(), platform))
	if err := javadist.InstallArchive(c.JdkFile, filepath.Base(c.JdkFile), root); err != nil {
		return jdk{}, fmt.Errorf("--jdk-file: %w", err)
	}
	major, _ := spec.Major()
	return jdk{label: spec.String(), major: major, home: javadist.Home(root)}, nil
}

// checkRuns runs the installed java, so a JDK built for another libc or
// architecture fails here rather than in a later step, and checks a
// --jdk-file JDK is the major version it was said to be. A JDK installed for
// another architecture is not run: it may be there only for a toolchain.
func checkRuns(j jdk, platform javadist.Platform) error {
	host, err := javadist.Host("")
	if err != nil || host.Arch != platform.Arch {
		return nil
	}
	_, reported, err := javapath.Probe(javadist.Java(j.home))
	if err != nil {
		return fmt.Errorf("the installed JDK does not run on this machine (%s): %w", platform, err)
	}
	spec, err := javaversion.Parse(reported)
	if err != nil {
		return nil
	}
	if major, ok := spec.Major(); ok && major != j.major {
		return fmt.Errorf("the JDK at %s is Java %s, not Java %d", j.home, reported, j.major)
	}
	return nil
}

// restore reports whether root holds an install, from an earlier run or,
// failing that, the job cache. A restore that fails part way is removed
// rather than trusted.
func (c *installCmd) restore(ctx context.Context, client *sdk.Client, key, label, root string) bool {
	if javadist.Installed(root) {
		return true
	}
	if !c.Cache {
		return false
	}
	_, err := client.RestoreCache(ctx, []string{key}, sdk.WithStepName("Restoring Java "+label))
	sdk.LogCacheErr("restore", err)
	if err != nil {
		_ = os.RemoveAll(root)
		return false
	}
	if !javadist.Installed(root) {
		return false
	}
	fmt.Fprintf(os.Stderr, "setup-java: restored Java %s from cache\n", label)
	return true
}

// installName names the directory an install lives in, e.g.
// temurin-jdk-21.0.5_11-linux-x64-glibc.
func (c *installCmd) installName(version string, platform javadist.Platform) string {
	return fmt.Sprintf("%s-%s-%s-%s", c.Distribution, c.JavaPackage, version, platform)
}

// cacheKey returns the key the install at root is cached under. The cache
// restores to the absolute path it saved, so the key carries a hash of root:
// executors whose $HOME differs must not share an entry that restores
// somewhere only one of them looks.
func (c *installCmd) cacheKey(version string, platform javadist.Platform, root string) string {
	sum := sha256.Sum256([]byte(root))
	key := fmt.Sprintf("setup-java-jdk-v1-%s-%x", c.installName(version, platform), sum[:4])
	if c.CacheNonce != "" {
		key += "-" + c.CacheNonce
	}
	return key
}

// export writes vars to $BASH_ENV, and reports them. Outside a job there may
// be no $BASH_ENV to write to, which is worth a warning, not a failure: the
// JDK is installed either way.
func export(vars map[string]string) {
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	slices.Sort(names)
	if err := javapath.Export(vars); err != nil {
		fmt.Fprintf(os.Stderr, "setup-java: warning: %v\n", err)
		return
	}
	for _, name := range names {
		fmt.Fprintf(os.Stderr, "setup-java: exported %s=%s to later steps (bash only)\n", name, vars[name])
	}
}

// expandHome resolves a leading ~ against $HOME. kong's path type resolves it
// against the passwd entry instead, which ignores a redirected $HOME.
func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return filepath.Abs(path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}
