package javaversion

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

func TestFromFile(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		content string
		want    FileVersion
	}{
		{name: "java-version major", file: ".java-version", content: "21\n", want: FileVersion{Spec: "21"}},
		{name: "java-version full", file: ".java-version", content: "21.0.5+11\n", want: FileVersion{Spec: "21.0.5+11"}},
		{name: "java-version with vendor prefix", file: ".java-version", content: "temurin-17.0.2\n", want: FileVersion{Spec: "17.0.2"}},
		{name: "java-version old notation", file: ".java-version", content: "1.8.0_292\n", want: FileVersion{Spec: "1.8.0_292"}},
		{name: "any other name", file: "jdk.txt", content: "17", want: FileVersion{Spec: "17"}},
		{name: "tool-versions bare", file: ".tool-versions", content: "nodejs 22.1.0\njava 21.0.5\n", want: FileVersion{Spec: "21.0.5"}},
		{
			name: "tool-versions temurin", file: ".tool-versions", content: "java temurin-21.0.5+11.0.LTS\n",
			want: FileVersion{Spec: "21.0.5+11", Distribution: "temurin", Vendor: "temurin"},
		},
		{
			name: "tool-versions vendor with digits", file: ".tool-versions", content: "java semeru-openj9-11.0.25+9\n",
			want: FileVersion{Spec: "11.0.25+9", Distribution: "semeru", Vendor: "semeru-openj9"},
		},
		{
			name: "tool-versions graalvm community", file: ".tool-versions", content: "java graalvm-community-21.0.2\n",
			want: FileVersion{Spec: "21.0.2", Distribution: "graalvm-community", Vendor: "graalvm-community"},
		},
		{
			name: "tool-versions unknown vendor", file: ".tool-versions", content: "java openjdk-21\n",
			want: FileVersion{Spec: "21", Vendor: "openjdk"},
		},
		{
			name: "sdkmanrc", file: ".sdkmanrc", content: "# comment\njava=21.0.5-tem\nmaven=3.9.6\n",
			want: FileVersion{Spec: "21.0.5", Distribution: "temurin", Vendor: "tem"},
		},
		{name: "sdkmanrc no vendor", file: ".sdkmanrc", content: "java=17\n", want: FileVersion{Spec: "17"}},
		{
			name: "sdkmanrc unknown vendor", file: ".sdkmanrc", content: "java=21.0.5-xyz\n",
			want: FileVersion{Spec: "21.0.5", Vendor: "xyz"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.file)
			assert.NilError(t, os.WriteFile(path, []byte(tc.content), 0600))
			got, err := FromFile(path)
			assert.NilError(t, err)
			assert.DeepEqual(t, got, tc.want)
			_, err = Parse(got.Spec)
			assert.NilError(t, err, "the spec read from the file must parse")
		})
	}
}

func TestFromFile_NoVersion(t *testing.T) {
	tests := []struct {
		file    string
		content string
	}{
		{file: ".java-version", content: "\n"},
		{file: ".tool-versions", content: "nodejs 22.1.0\n"},
		{file: ".sdkmanrc", content: "maven=3.9.6\n"},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.file)
			assert.NilError(t, os.WriteFile(path, []byte(tc.content), 0600))
			_, err := FromFile(path)
			assert.ErrorContains(t, err, "no Java version found")
		})
	}
}

func TestFromFile_Missing(t *testing.T) {
	_, err := FromFile(filepath.Join(t.TempDir(), ".java-version"))
	assert.Assert(t, os.IsNotExist(err))
}
