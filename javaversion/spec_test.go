package javaversion

import (
	"testing"

	"gotest.tools/v3/assert"
)

func v(major, security, patch, build int) Version {
	return Version{Major: major, Security: security, Patch: patch, Build: build}
}

func ea28(build int) Version {
	return Version{Major: 28, Build: build, EA: true}
}

func TestVersion_String(t *testing.T) {
	tests := []struct {
		name string
		v    Version
		want string
	}{
		{name: "release", v: v(21, 5, 0, 11), want: "21.0.5+11"},
		{name: "patch", v: v(21, 12, 1, 1), want: "21.0.12.1+1"},
		{name: "java 8", v: v(8, 504, 0, 1), want: "8.0.504+1"},
		{name: "early access", v: ea28(19), want: "28-ea+19"},
		{name: "no build", v: v(21, 5, 0, 0), want: "21.0.5"},
		{name: "vendor revision", v: Version{Major: 21, Security: 12, Build: 12, Revision: 1}, want: "21.0.12+12.1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.v.String(), tc.want)
		})
	}
	assert.Equal(t, v(21, 5, 0, 11).DirName(), "21.0.5_11")
}

func TestParse_Invalid(t *testing.T) {
	for _, raw := range []string{"", "  ", "latest-ea", "0", "0+35", "banana", ">=foo"} {
		t.Run(raw, func(t *testing.T) {
			_, err := Parse(raw)
			assert.Assert(t, err != nil, "expected %q to be rejected", raw)
		})
	}
}

func TestParse_String(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{raw: "21", want: "21"},
		{raw: " 21.0.5 ", want: "21.0.5"},
		{raw: "21.0.5+11", want: "21.0.5+11"},
		{raw: "1.8", want: "8"},
		{raw: "1.8.0_292", want: "8.0.292"},
		{raw: "1.8.0_292-b10", want: "8.0.292+10"},
		{raw: "LATEST", want: "latest"},
		{raw: "26-ea", want: "26-ea"},
		{raw: "11.0.3-ea.2", want: "11.0.3-ea+2"},
		{raw: "28-ea+19", want: "28.0.0-ea+19"},
		{raw: "21+35", want: "21.0.0+35"},
		{raw: ">=17 <22", want: ">=17 <22"},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			s, err := Parse(tc.raw)
			assert.NilError(t, err)
			assert.Equal(t, s.String(), tc.want)
		})
	}
}

func TestSpec_Resolve(t *testing.T) {
	candidates := []Version{
		v(8, 502, 0, 7),
		v(8, 504, 0, 1),
		v(17, 13, 0, 11),
		v(17, 14, 0, 7),
		v(21, 0, 0, 35),
		v(21, 5, 0, 11),
		v(21, 12, 0, 8),
		v(21, 12, 1, 1),
		v(25, 1, 0, 8),
		ea28(18),
		ea28(19),
	}
	tests := []struct {
		spec string
		want string
	}{
		{spec: "21", want: "21.0.12.1+1"},
		{spec: "21.0", want: "21.0.12.1+1"},
		{spec: "21.0.5", want: "21.0.5+11"},
		{spec: "21.0.12+8", want: "21.0.12+8"},
		{spec: "21.0.12.1", want: "21.0.12.1+1"},
		{spec: "21.0.12.1+1", want: "21.0.12.1+1"},
		{spec: "8", want: "8.0.504+1"},
		{spec: "1.8", want: "8.0.504+1"},
		{spec: "1.8.0_502", want: "8.0.502+7"},
		{spec: "latest", want: "25.0.1+8"},
		{spec: ">=17 <21", want: "17.0.14+7"},
		{spec: "17.x", want: "17.0.14+7"},
		{spec: "28-ea", want: "28-ea+19"},
		{spec: "28-ea+18", want: "28-ea+18"},
		{spec: "21+35", want: "21.0.0+35"},
	}
	for _, tc := range tests {
		t.Run(tc.spec, func(t *testing.T) {
			s, err := Parse(tc.spec)
			assert.NilError(t, err)
			got, err := s.Resolve(candidates)
			assert.NilError(t, err)
			assert.Equal(t, got.String(), tc.want)
		})
	}
}

func TestSpec_Resolve_NoMatch(t *testing.T) {
	for _, raw := range []string{"22", "21.0.6", "21.0.12+9", "17-ea", ">=30"} {
		t.Run(raw, func(t *testing.T) {
			s, err := Parse(raw)
			assert.NilError(t, err)
			_, err = s.Resolve([]Version{v(21, 12, 0, 8), ea28(19)})
			assert.ErrorIs(t, err, ErrNoMatch)
		})
	}
}

func TestSpec_Fields(t *testing.T) {
	tests := []struct {
		raw      string
		major    int
		hasMajor bool
		exact    string
		concrete bool
	}{
		{raw: "21", major: 21, hasMajor: true, concrete: true},
		{raw: "21.0.5+11", major: 21, hasMajor: true, exact: "21.0.5+11", concrete: true},
		{raw: "21.0.12.1+1", major: 21, hasMajor: true, exact: "21.0.12.1+1", concrete: true},
		{raw: "28-ea+19", major: 28, hasMajor: true, exact: "28-ea+19", concrete: true},
		{raw: "latest"},
		{raw: ">=17"},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			s, err := Parse(tc.raw)
			assert.NilError(t, err)
			major, ok := s.Major()
			assert.Equal(t, ok, tc.hasMajor)
			assert.Equal(t, major, tc.major)
			exact, ok := s.Exact()
			assert.Equal(t, ok, tc.exact != "")
			if ok {
				assert.Equal(t, exact.String(), tc.exact)
			}
			assert.Equal(t, s.Concrete(), tc.concrete)
		})
	}
}
