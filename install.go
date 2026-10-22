package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/circleci-functions/toolkit/sdk"

	"github.com/circleci-functions/setup-java/depcache"
	"github.com/circleci-functions/setup-java/javadist"
	"github.com/circleci-functions/setup-java/javapath"
	"github.com/circleci-functions/setup-java/javaversion"
)

const jdkFileDistribution = "jdkfile"

// installCmd installs one or more JDKs, makes the last of them the one later
// steps run, and exports each one's home by major version.
type installCmd struct {
	JavaVersion       []string `help:"Java version to install: a major (21), a full version (21.0.5 or 21.0.5+11), a semver range (>=17 <22), or latest. Add -ea for an early-access build (26-ea). Separate several with commas to install each; the last becomes the default." example:"21"`
	JavaVersionFile   string   `help:"File to read the Java version from: .java-version, .tool-versions or .sdkmanrc. When --java-version is set too, the two must ask for the same version." type:"path"`
	Distribution      string   `help:"Java distribution to install: ${distribution_list}, or jdkfile to install the archive given by --jdk-file." enum:"${distributions},jdkfile" required:"" example:"temurin"`
	JavaPackage       string   `help:"Package type to install: jdk or jre, or for some distributions a variant such as jdk+fx." default:"jdk"`
	Architecture      string   `help:"Architecture to install for: x64, x86, aarch64, armv7, ppc64le, ppc64, riscv64 or s390x (amd64 and arm64 work too). Defaults to the machine's."`
	JdkFile           string   `help:"JDK archive to install (.tar.gz, .tgz or .zip), with --distribution jdkfile." type:"path"`
	CacheDependencies string   `help:"Also cache what this build tool downloads: ${build_tool_list}. It is restored now, and saved at the end of the job if the job succeeds." enum:"${build_tools}," default:""`
	depCacheFlags     `embed:""`
	MakeDefaultJdk    bool   `help:"Make this JDK the one java, javac and JAVA_HOME resolve to. Set to false to install it only for build-tool toolchains, which find it through JAVA_HOME_<major>_<ARCH>." default:"true"`
	Cache             bool   `help:"Restore the JDK from the job cache, and save it after a fresh download. Set to false to disable caching." default:"true"`
	InstallDir        string `help:"Directory to install JDKs under." default:"~/.cache/setup-java" hidden:""`
	BinDir            string `help:"Link the JDK tools into this directory instead of discovering one on PATH." type:"path" hidden:""`
	CacheNonce        string `help:"Partition the JDK cache, so a test run cannot restore an earlier run's install." hidden:""`
	APIURL            string `name:"api-url" help:"Look builds up under this URL instead of the distribution's own." hidden:""`
}

type jdk struct {
	label string
	major int
	home  string
	// env holds the variables, beyond JAVA_HOME, the JDK needs when it is the
	// default.
	env map[string]string
}

func (c *installCmd) Run(ctx context.Context, client *sdk.Client) error {
	specs, err := c.specs()
	if err != nil {
		return err
	}
	var deps []depcache.Entry
	switch {
	case c.CacheDependencies != "":
		if deps, err = c.planDeps(c.CacheDependencies, c.CacheNonce); err != nil {
			return err
		}
	case c.set():
		return errors.New("--cache-dependency-path, --cache-path and --cache-read-only need --cache-dependencies")
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
		maps.Copy(vars, def.env)
	} else {
		fmt.Fprintf(os.Stderr, "setup-java: --make-default-jdk=false, so java and JAVA_HOME are left as they were\n")
	}
	export(vars)
	if len(deps) > 0 {
		c.restoreDeps(ctx, client, deps)
	}
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
		dist := c.distribution()
		for _, spec := range specs {
			if err := javadist.CheckSpec(dist, spec); err != nil {
				return err
			}
		}
		return javadist.CheckPackage(dist, c.JavaPackage)
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

// distribution returns the --distribution to install from. kong has already
// checked the name is one of javadist.Names.
func (c *installCmd) distribution() javadist.Distribution {
	d, _ := javadist.New(c.Distribution, c.APIURL)
	return d
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
		if r.Floating {
			fmt.Fprintf(os.Stderr, "setup-java: resolved %q to the newest %s build of Java %d (%s)\n", spec, dist.Name(), r.Version.Major, platform)
			return nil
		}
		fmt.Fprintf(os.Stderr, "setup-java: resolved %q to %s %s (%s)\n", spec, dist.Name(), r.Version, platform)
		return nil
	}

	if !exact {
		if err := resolve(); err != nil {
			return jdk{}, err
		}
		version = release.Version
	}
	id, label := version.DirName(), version.String()
	if release.Floating {
		id, label = fmt.Sprintf("%d-%s", version.Major, release.SHA256[:12]), strconv.Itoa(version.Major)
	}
	root := filepath.Join(installDir, c.installName(id, platform))
	j := jdk{label: label, major: version.Major}
	key := c.cacheKey(id, platform, root)
	if !c.restore(ctx, client, key, j.label, root) {
		if exact {
			if err := resolve(); err != nil {
				return jdk{}, err
			}
		}
		if err := javadist.Install(ctx, release, root); err != nil {
			return jdk{}, err
		}
		if c.Cache {
			sdk.LogCacheErr("save", client.SaveCache(ctx, key, []string{root}, sdk.WithStepName("Saving Java "+j.label)))
		}
	}
	j.home = javadist.Home(root)
	j.env = javadist.Env(dist, j.home)
	if release.Floating {
		v, err := javadist.ReleaseVersion(j.home)
		if err != nil {
			return jdk{}, err
		}
		j.label = v.String()
	}
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
