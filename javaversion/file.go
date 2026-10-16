package javaversion

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// FileVersion is what a version file asks for.
type FileVersion struct {
	Spec string
	// Distribution is the distribution the file names, in setup-java's names,
	// or "" when it names none or one setup-java does not know.
	Distribution string
	// Vendor is the distribution identifier as written in the file, or "".
	Vendor string
}

var (
	// toolVersionsLine is asdf's `java [vendor-]version`, e.g.
	// `java temurin-21.0.5+11.0.LTS` or `java 21`.
	toolVersionsLine = regexp.MustCompile(`(?m)^java\s+(\S+)`)
	// sdkmanLine is SDKMAN's `java=version[-vendor]`, e.g. `java=21.0.5-tem`.
	sdkmanLine = regexp.MustCompile(`(?m)^java\s*=\s*([^-\s]+)(?:-([A-Za-z0-9]+))?`)
	// leadingVersion takes the version off a token that may carry a vendor
	// prefix or suffixes: 21, 21.0.5+11, 1.8.0_292, 17-ea.
	leadingVersion = regexp.MustCompile(`(\d+(?:\.\d+)*(?:_\d+)?(?:-ea)?(?:\+\d+)?)`)
	// vendorVersion finds the version in an asdf-java token, where a vendor
	// part may hold digits too, as in semeru-openj9-11.0.25+9.
	vendorVersion = regexp.MustCompile(`(?:^|-)(\d+(?:\.\d+)*(?:_\d+)?(?:-ea)?(?:\+\d+)?)`)
	// versionToken is the first token in a .java-version file that starts
	// with a digit.
	versionToken = regexp.MustCompile(`(?:^|[\s-])(\d\S*)`)
)

var sdkmanVendors = map[string]string{
	"tem":        "temurin",
	"sem":        "semeru",
	"albba":      "dragonwell",
	"dragonwell": "dragonwell",
	"zulu":       "zulu",
	"amzn":       "corretto",
	"graal":      "graalvm",
	"graalce":    "graalvm-community",
	"librca":     "liberica",
	"ms":         "microsoft",
	"oracle":     "oracle",
	"sapmchn":    "sapmachine",
	"jbr":        "jetbrains",
	"kona":       "kona",
	"open":       "oracle-openjdk",
}

var asdfVendors = map[string]string{
	"temurin":      "temurin",
	"adoptopenjdk": "temurin",
	"zulu":         "zulu",
	"corretto":     "corretto",
	"liberica":     "liberica",
	"microsoft":    "microsoft",
	"semeru":       "semeru",
	"ibm":          "semeru",
	"dragonwell":   "dragonwell",
	"graalvm":      "graalvm",
	"oracle":       "oracle",
	"sapmachine":   "sapmachine",
	"kona":         "kona",
	"jetbrains":    "jetbrains",
	"openjdk":      "oracle-openjdk",
}

// FromFile reads the Java version from a .java-version, .tool-versions or
// .sdkmanrc file. Any other file name is read like .java-version.
func FromFile(path string) (FileVersion, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the caller names the file to read
	if err != nil {
		return FileVersion{}, err
	}
	fv, ok := parseFile(filepath.Base(path), string(data))
	if !ok {
		return FileVersion{}, fmt.Errorf("no Java version found in %s", path)
	}
	return fv, nil
}

func parseFile(name, content string) (FileVersion, bool) {
	switch name {
	case ".tool-versions":
		m := toolVersionsLine.FindStringSubmatch(content)
		if m == nil {
			return FileVersion{}, false
		}
		return fromToolVersions(m[1])
	case ".sdkmanrc":
		m := sdkmanLine.FindStringSubmatch(content)
		if m == nil {
			return FileVersion{}, false
		}
		v := leadingVersion.FindString(m[1])
		if v == "" {
			return FileVersion{}, false
		}
		return FileVersion{Spec: v, Distribution: sdkmanVendors[strings.ToLower(m[2])], Vendor: m[2]}, true
	}
	m := versionToken.FindStringSubmatch(strings.TrimSpace(content))
	if m == nil {
		return FileVersion{}, false
	}
	v := leadingVersion.FindString(m[1])
	return FileVersion{Spec: v}, v != ""
}

// fromToolVersions splits an asdf-java token into its vendor and version:
// temurin-21.0.5+11.0.LTS, semeru-openj9-11.0.25+9, graalvm-community-21.0.2
// or a bare 21.
func fromToolVersions(token string) (FileVersion, bool) {
	loc := vendorVersion.FindStringSubmatchIndex(token)
	if loc == nil {
		return FileVersion{}, false
	}
	fv := FileVersion{Spec: token[loc[2]:loc[3]]}
	vendor := strings.TrimSuffix(token[:loc[2]], "-")
	if vendor == "" {
		return fv, true
	}
	fv.Vendor = vendor
	lower := strings.ToLower(vendor)
	switch {
	case strings.HasPrefix(lower, "graalvm-community"):
		fv.Distribution = "graalvm-community"
	case strings.HasPrefix(lower, "oracle-graalvm"):
		fv.Distribution = "graalvm"
	default:
		base, _, _ := strings.Cut(lower, "-")
		fv.Distribution = asdfVendors[base]
	}
	return fv, true
}
