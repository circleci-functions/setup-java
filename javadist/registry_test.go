package javadist_test

import (
	"slices"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/setup-java/javadist"
	"github.com/circleci-functions/setup-java/javaversion"
)

func TestNew(t *testing.T) {
	names := javadist.Names()
	assert.Assert(t, slices.IsSorted(names), "%v", names)
	for _, name := range names {
		d, ok := javadist.New(name, "")
		assert.Assert(t, ok, name)
		assert.Equal(t, d.Name(), name)
		assert.Assert(t, len(d.Packages()) > 0, name)
	}
	_, ok := javadist.New("jdkfile", "")
	assert.Assert(t, !ok, "jdkfile installs a local archive, not a distribution's builds")
}

func TestCheckSpec(t *testing.T) {
	ea, err := javaversion.Parse("28-ea")
	assert.NilError(t, err)
	release, err := javaversion.Parse("21")
	assert.NilError(t, err)

	temurin, _ := javadist.New("temurin", "")
	assert.NilError(t, javadist.CheckSpec(temurin, ea))
	assert.NilError(t, javadist.CheckSpec(temurin, release))

	zulu, _ := javadist.New("zulu", "")
	assert.ErrorContains(t, javadist.CheckSpec(zulu, ea), "setup-java installs no early-access builds of zulu")
	assert.NilError(t, javadist.CheckSpec(zulu, release))
}
