package javadist

import (
	"slices"
	"strings"
)

// distributions maps each distribution's name to a constructor taking the
// base URL to look builds up in, or "" for the vendor's own.
var distributions = map[string]func(apiURL string) Distribution{
	"temurin":  func(apiURL string) Distribution { return Temurin{BaseURL: cmpOr(apiURL, AdoptiumURL)} },
	"zulu":     func(apiURL string) Distribution { return Zulu{BaseURL: cmpOr(apiURL, AzulURL)} },
	"corretto": newCorretto,
}

// Names returns the names of the supported distributions, sorted.
func Names() []string {
	names := make([]string, 0, len(distributions))
	for name := range distributions {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// New returns the named distribution, looking builds up under apiURL when it
// is set. It reports false for a name it does not know.
func New(name, apiURL string) (Distribution, bool) {
	newDist, ok := distributions[name]
	if !ok {
		return nil, false
	}
	return newDist(strings.TrimSuffix(apiURL, "/")), true
}

func cmpOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
