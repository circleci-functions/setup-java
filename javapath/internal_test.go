package javapath

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

func TestTakesOver(t *testing.T) {
	otherJDK := t.TempDir()
	preferred := t.TempDir()
	earlierRun := t.TempDir()
	empty := t.TempDir()
	for _, dir := range []string{otherJDK, earlierRun} {
		assert.NilError(t, os.WriteFile(filepath.Join(dir, exe("java")), nil, 0700)) //nolint:gosec // must look executable
	}
	writable := func(string) bool { return true }

	tests := []struct {
		name      string
		dirs      []string
		preferred []string
		want      string
	}{
		{name: "another JDK first, nothing ahead of it", dirs: []string{otherJDK, preferred}, preferred: []string{preferred}, want: otherJDK},
		{name: "a preferred directory ahead of it", dirs: []string{preferred, otherJDK}, preferred: []string{preferred}},
		{name: "a link from an earlier run", dirs: []string{earlierRun, otherJDK}, preferred: []string{earlierRun}},
		{name: "no java on PATH", dirs: []string{empty}, preferred: []string{preferred}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := takesOver(tc.dirs, tc.preferred, writable)
			assert.Equal(t, ok, tc.want != "")
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestUnixPath(t *testing.T) {
	tests := map[string]string{
		`C:\Users\circleci\.cache\setup-java\bin`: "/c/Users/circleci/.cache/setup-java/bin",
		`D:/jdk/bin`:   "/d/jdk/bin",
		"/opt/jdk/bin": "/opt/jdk/bin",
	}
	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			assert.Equal(t, unixPath(in), want)
		})
	}
}

func exe(name string) string {
	if filepath.Separator == '\\' {
		return name + ".exe"
	}
	return name
}
