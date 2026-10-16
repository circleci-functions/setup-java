package javaversion

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
)

var ErrNoMatch = errors.New("no matching Java version")

// Spec selects Java versions: a version or the start of one (21, 21.0.5,
// 21.0.5+11, 21+35, 18.0.1.1), a semver range (>=17 <22, 21.x), or latest. A -ea
// suffix selects early-access builds instead of releases.
type Spec struct {
	raw    string
	latest bool
	ea     bool
	// fields are the leading version fields the spec fixes: major, then
	// minor, security and patch.
	fields []int
	// build is the build number the spec fixes, or 0 for any.
	build int
	rng   *semver.Constraints
}

var (
	exactForm = regexp.MustCompile(`^(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:\.(\d+))?(?:\+(\d+))?$`)
	// oldForm is the pre-Java 9 notation, e.g. 1.8, 1.8.0_292 or
	// 1.8.0_292-b10.
	oldForm = regexp.MustCompile(`^1\.(\d+)(?:\.0)?(?:_(\d+))?(?:-b(\d+))?$`)
)

func Parse(raw string) (Spec, error) {
	s := strings.TrimSpace(raw)
	spec := Spec{raw: s}
	lower := strings.ToLower(s)
	switch {
	case s == "":
		return Spec{}, errors.New("empty Java version")
	case lower == "latest":
		spec.latest = true
		return spec, nil
	case strings.HasPrefix(lower, "latest"):
		return Spec{}, fmt.Errorf("java version %q: latest selects the newest release and takes no suffix", raw)
	}

	if base, build, ok := strings.Cut(s, "-ea."); ok {
		s = base + "+" + build
		spec.ea = true
	} else if base, build, ok := strings.Cut(s, "-ea+"); ok {
		s = base + "+" + build
		spec.ea = true
	} else if base, ok := strings.CutSuffix(s, "-ea"); ok {
		s = base
		spec.ea = true
	}

	if m := oldForm.FindStringSubmatch(s); m != nil {
		s = m[1]
		if m[2] != "" {
			s += ".0." + m[2]
		}
		if m[3] != "" {
			s += "+" + m[3]
		}
	}

	if m := exactForm.FindStringSubmatch(s); m != nil {
		return spec.withFields(raw, m)
	}

	rng, err := semver.NewConstraint(s)
	if err != nil {
		return Spec{}, fmt.Errorf("java version %q is not a version, a range or latest", raw)
	}
	spec.rng = rng
	return spec, nil
}

func (s Spec) withFields(raw string, m []string) (Spec, error) {
	for _, f := range m[1:5] {
		if f == "" {
			break
		}
		n, err := atoi(f)
		if err != nil {
			return Spec{}, err
		}
		s.fields = append(s.fields, n)
	}
	if m[5] != "" {
		s.build, _ = strconv.Atoi(m[5])
		for len(s.fields) < 3 {
			s.fields = append(s.fields, 0)
		}
	}
	if s.fields[0] == 0 {
		return Spec{}, fmt.Errorf("java version %q: there is no Java 0", raw)
	}
	return s, nil
}

// String returns the spec in a normal form, so two ways of writing the same
// spec (1.8 and 8) compare equal.
func (s Spec) String() string {
	switch {
	case s.latest:
		return "latest"
	case s.rng != nil:
		return s.raw
	}
	parts := make([]string, len(s.fields))
	for i, f := range s.fields {
		parts[i] = strconv.Itoa(f)
	}
	out := strings.Join(parts, ".")
	if s.ea {
		out += "-ea"
	}
	if s.build > 0 {
		out += "+" + strconv.Itoa(s.build)
	}
	return out
}

func (s Spec) EA() bool {
	return s.ea
}

func (s Spec) Latest() bool {
	return s.latest
}

// Major returns the major version the spec fixes, if it fixes one.
func (s Spec) Major() (int, bool) {
	if len(s.fields) == 0 {
		return 0, false
	}
	return s.fields[0], true
}

// Exact returns the one version the spec can match, when it fixes every field
// down to the build.
func (s Spec) Exact() (Version, bool) {
	if s.build == 0 || len(s.fields) < 3 {
		return Version{}, false
	}
	v := Version{Major: s.fields[0], Minor: s.fields[1], Security: s.fields[2], Build: s.build, EA: s.ea}
	if len(s.fields) == 4 {
		v.Patch = s.fields[3]
	}
	return v, true
}

// Concrete reports whether the spec names a version rather than a range or
// latest, so it can stand for a JDK whose version is not looked up.
func (s Spec) Concrete() bool {
	return len(s.fields) > 0
}

func (s Spec) Matches(v Version) bool {
	if v.EA != s.ea {
		return false
	}
	switch {
	case s.latest:
		return true
	case s.rng != nil:
		sv, err := semver.NewVersion(fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Security))
		return err == nil && s.rng.Check(sv)
	}
	if !slices.Equal(v.fields()[:len(s.fields)], s.fields) {
		return false
	}
	if len(s.fields) == 3 && s.build > 0 && v.Patch != 0 {
		return false
	}
	return s.build == 0 || v.Build == s.build
}

// Resolve returns the newest of candidates that satisfies the spec.
func (s Spec) Resolve(candidates []Version) (Version, error) {
	var best Version
	found := false
	for _, v := range candidates {
		if s.Matches(v) && (!found || v.Compare(best) > 0) {
			best, found = v, true
		}
	}
	if !found {
		return Version{}, fmt.Errorf("%w for %q", ErrNoMatch, s.raw)
	}
	return best, nil
}
