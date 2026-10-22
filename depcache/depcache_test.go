package depcache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		assert.NilError(t, os.MkdirAll(filepath.Dir(path), 0750))
		assert.NilError(t, os.WriteFile(path, []byte(content), 0600))
	}
	return dir
}

func plan(t *testing.T, tool string, opts Options) []Entry {
	t.Helper()
	tl, ok := Lookup(tool)
	assert.Assert(t, ok, tool)
	opts.Platform = "linux-amd64"
	entries, err := Plan(tl, opts)
	assert.NilError(t, err)
	return entries
}

func TestPlan_Maven(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := writeProject(t, map[string]string{
		"pom.xml":                               "<project/>",
		"module/pom.xml":                        "<project/>",
		".mvn/wrapper/maven-wrapper.properties": "distributionUrl=x",
		"src/main/java/App.java":                "class App {}",
	})

	entries := plan(t, "maven", Options{ProjectDir: dir})
	assert.Equal(t, len(entries), 2)
	main, wrapper := entries[0], entries[1]
	assert.Equal(t, main.Label, "Maven dependencies")
	assert.Assert(t, strings.HasPrefix(main.Key, "setup-java-v1-maven-linux-amd64-"), main.Key)
	assert.DeepEqual(t, main.RestoreKeys, []string{main.Key, "setup-java-v1-maven-linux-amd64-"})
	assert.DeepEqual(t, main.Paths, []string{filepath.Join(home, ".m2", "repository")})
	assert.Assert(t, strings.HasPrefix(wrapper.Key, "setup-java-v1-maven-wrapper-linux-amd64-"), wrapper.Key)
	assert.DeepEqual(t, wrapper.Paths, []string{filepath.Join(home, ".m2", "wrapper", "dists")})
	assert.Assert(t, !strings.HasPrefix(wrapper.Key, main.RestoreKeys[1]), "the main cache's fallback must not restore the wrapper cache")
}

func TestPlan_KeyFollowsBuildFiles(t *testing.T) {
	files := map[string]string{
		"pom.xml":                               "<project/>",
		".mvn/wrapper/maven-wrapper.properties": "distributionUrl=x",
		"README.md":                             "hello",
	}
	before := plan(t, "maven", Options{ProjectDir: writeProject(t, files)})

	files["README.md"] = "changed"
	assert.DeepEqual(t, plan(t, "maven", Options{ProjectDir: writeProject(t, files)}), before)

	files["pom.xml"] = "<project><dependencies/></project>"
	after := plan(t, "maven", Options{ProjectDir: writeProject(t, files)})
	assert.Assert(t, after[0].Key != before[0].Key, "a changed pom.xml changes the dependency cache's key")
	assert.Equal(t, after[1].Key, before[1].Key, "but not the wrapper's")
}

func TestPlan_Gradle(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  int
	}{
		{name: "groovy", files: map[string]string{"build.gradle": "", "settings.gradle": ""}, want: 1},
		{name: "kotlin with a wrapper", files: map[string]string{"build.gradle.kts": "", "gradle/wrapper/gradle-wrapper.properties": ""}, want: 2},
		{name: "version catalog only", files: map[string]string{"gradle/libs.versions.toml": ""}, want: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, len(plan(t, "gradle", Options{ProjectDir: writeProject(t, tc.files)})), tc.want)
		})
	}
}

func TestPlan_GradleUserHome(t *testing.T) {
	gradleHome := t.TempDir()
	t.Setenv("GRADLE_USER_HOME", gradleHome)
	entries := plan(t, "gradle", Options{ProjectDir: writeProject(t, map[string]string{"build.gradle": ""})})
	assert.DeepEqual(t, entries[0].Paths, []string{filepath.Join(gradleHome, "caches")})
}

func TestPlan_Overrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := writeProject(t, map[string]string{"pom.xml": "a", "backend/deps.lock": "b"})

	entries := plan(t, "maven", Options{
		ProjectDir: dir,
		Files:      []string{"backend/deps.lock"},
		Dirs:       []string{"~/custom-repo", "local-repo", filepath.Join(home, "abs")},
	})
	assert.DeepEqual(t, entries[0].Paths, []string{
		filepath.Join(home, "custom-repo"),
		filepath.Join(dir, "local-repo"),
		filepath.Join(home, "abs"),
	})
	assert.Assert(t, entries[0].Key != plan(t, "maven", Options{ProjectDir: dir})[0].Key, "the key comes from the files asked for")
}

func TestPlan_Nonce(t *testing.T) {
	entries := plan(t, "maven", Options{ProjectDir: writeProject(t, map[string]string{"pom.xml": ""}), Nonce: "run1"})
	assert.Equal(t, entries[0].RestoreKeys[1], "setup-java-v1-maven-linux-amd64-run1-", "the fallback stays inside the nonce")
}

func TestPlan_NoBuildFiles(t *testing.T) {
	dir := writeProject(t, map[string]string{"README.md": "", ".git/pom.xml": ""})
	tl, _ := Lookup("maven")
	_, err := Plan(tl, Options{ProjectDir: dir, Platform: "linux-amd64"})
	assert.ErrorIs(t, err, ErrNoBuildFiles)
	assert.ErrorContains(t, err, "no build files found for maven in "+dir+": looked for **/pom.xml")
}

func TestNames(t *testing.T) {
	assert.DeepEqual(t, Names(), []string{"gradle", "maven"})
}
