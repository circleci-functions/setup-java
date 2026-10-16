// Package javaversion parses Java versions and the version specs that select
// them, and reads specs from version files.
package javaversion

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"
)

// Version is a Java release, in the fields of JEP 322's $FEATURE.$INTERIM.
// $UPDATE.$PATCH+$BUILD scheme. Java 8 maps onto it as 8.0.<update>+<build>.
type Version struct {
	Major, Minor, Security, Patch, Build int
	// Revision is a vendor's own respin of a build, as in Corretto's
	// 21.0.12.12.1, or 0.
	Revision int
	EA       bool
}

// String returns the version as OpenJDK writes it, e.g. 21.0.5+11,
// 21.0.12.1+1 or 26-ea+19, with any vendor revision after the build:
// 21.0.12+12.1.
func (v Version) String() string {
	var b strings.Builder
	b.WriteString(v.numbers())
	if v.EA {
		b.WriteString("-ea")
	}
	if v.Build > 0 {
		b.WriteString("+" + strconv.Itoa(v.Build))
	}
	if v.Revision > 0 {
		b.WriteString("." + strconv.Itoa(v.Revision))
	}
	return b.String()
}

// numbers returns the dotted part of the version. An EA build of a feature
// release prints as just its major, as OpenJDK writes it (26-ea).
func (v Version) numbers() string {
	fields := []int{v.Major, v.Minor, v.Security, v.Patch}
	n := 3
	if v.Patch > 0 {
		n = 4
	}
	if v.EA && v.Minor == 0 && v.Security == 0 && v.Patch == 0 {
		n = 1
	}
	parts := make([]string, n)
	for i := range n {
		parts[i] = strconv.Itoa(fields[i])
	}
	return strings.Join(parts, ".")
}

// DirName returns the version in a form safe for a directory name: some Java
// tools mishandle a "+" in a path.
func (v Version) DirName() string {
	return strings.ReplaceAll(v.String(), "+", "_")
}

// Compare orders versions oldest first. A release sorts after the
// early-access builds of the same version.
func (v Version) Compare(o Version) int {
	return cmp.Or(
		cmp.Compare(v.Major, o.Major),
		cmp.Compare(v.Minor, o.Minor),
		cmp.Compare(v.Security, o.Security),
		cmp.Compare(v.Patch, o.Patch),
		compareBool(!v.EA, !o.EA),
		cmp.Compare(v.Build, o.Build),
		cmp.Compare(v.Revision, o.Revision),
	)
}

func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

func (v Version) fields() []int {
	return []int{v.Major, v.Minor, v.Security, v.Patch}
}

func atoi(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%q is not a version number", s)
	}
	return n, nil
}
