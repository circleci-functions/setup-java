// Package depcache caches what a Java build tool downloads, keyed on the
// project's build files: restored when a job starts, and saved when it ends.
package depcache

import (
	"os"
	"path/filepath"
	"slices"
)

// Tool is a build tool whose downloads can be cached.
type Tool struct {
	Name string
	// Home returns the folder the tool keeps its downloads under, e.g.
	// ~/.m2.
	Home func() (string, error)
	// Caches are cached and keyed separately, the main one first.
	Caches []Cache
}

// Cache is a set of folders saved and restored together under one key.
type Cache struct {
	// ID names the cache in its key, e.g. maven or maven-wrapper.
	ID string
	// Label names the cache in step names, e.g. "Maven dependencies".
	Label string
	// Dirs are the folders to cache, relative to the tool's home.
	Dirs []string
	// Files are glob patterns, relative to the project, for the files whose
	// contents key the cache.
	Files []string
	// Optional marks a cache for a feature a project may not use, such as a
	// build tool wrapper: it is skipped when no file matches.
	Optional bool
}

var tools = map[string]Tool{
	"maven": {
		Name: "maven",
		Home: inHome(".m2"),
		Caches: []Cache{
			{
				ID:    "maven",
				Label: "Maven dependencies",
				Dirs:  []string{"repository"},
				Files: []string{"**/pom.xml", "**/.mvn/wrapper/maven-wrapper.properties", "**/.mvn/extensions.xml"},
			},
			{
				ID:       "maven-wrapper",
				Label:    "Maven wrapper",
				Dirs:     []string{filepath.Join("wrapper", "dists")},
				Files:    []string{"**/.mvn/wrapper/maven-wrapper.properties"},
				Optional: true,
			},
		},
	},
	"gradle": {
		Name: "gradle",
		Home: gradleHome,
		Caches: []Cache{
			{
				ID:    "gradle",
				Label: "Gradle dependencies",
				Dirs:  []string{"caches"},
				Files: []string{
					"**/*.gradle*",
					"**/gradle.properties",
					"**/gradle-wrapper.properties",
					"buildSrc/**/Versions.kt",
					"buildSrc/**/Dependencies.kt",
					"gradle/*.versions.toml",
					"**/versions.properties",
				},
			},
			{
				ID:       "gradle-wrapper",
				Label:    "Gradle wrapper",
				Dirs:     []string{"wrapper"},
				Files:    []string{"**/gradle-wrapper.properties"},
				Optional: true,
			},
		},
	},
}

// Names returns the names of the supported build tools, sorted.
func Names() []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Lookup returns the named build tool.
func Lookup(name string) (Tool, bool) {
	t, ok := tools[name]
	return t, ok
}

func inHome(dir string) func() (string, error) {
	return func() (string, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, dir), nil
	}
}

// gradleHome is $GRADLE_USER_HOME, where Gradle keeps its downloads when it
// is set, and ~/.gradle otherwise.
func gradleHome() (string, error) {
	if dir := os.Getenv("GRADLE_USER_HOME"); dir != "" {
		return dir, nil
	}
	return inHome(".gradle")()
}
