package javadist

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/circleci-functions/setup-java/javaversion"
)

// Release is a JDK build to download.
type Release struct {
	Version javaversion.Version
	URL     string
	// SHA256 is the archive's checksum as the vendor publishes it, or "" when
	// it publishes none.
	SHA256 string
}

// Distribution resolves version specs to builds from one vendor.
type Distribution interface {
	Name() string
	// Packages are the package types the distribution publishes, e.g. jdk
	// and jre.
	Packages() []string
	// Resolve returns the newest build of pkg for p that satisfies spec.
	Resolve(ctx context.Context, spec javaversion.Spec, p Platform, pkg string) (Release, error)
}

func CheckPackage(d Distribution, pkg string) error {
	if slices.Contains(d.Packages(), pkg) {
		return nil
	}
	return fmt.Errorf("%s does not publish a %q package: use one of %s", d.Name(), pkg, strings.Join(d.Packages(), ", "))
}
