package javadist

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	hc "github.com/circleci-functions/toolkit/httpcl"

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

// earlyAccess is implemented by a distribution that installs early-access
// builds.
type earlyAccess interface {
	EarlyAccess() bool
}

func CheckPackage(d Distribution, pkg string) error {
	if slices.Contains(d.Packages(), pkg) {
		return nil
	}
	return fmt.Errorf("%s does not publish a %q package: use one of %s", d.Name(), pkg, strings.Join(d.Packages(), ", "))
}

// CheckSpec returns an error when d cannot install what spec asks for at all,
// before anything is looked up.
func CheckSpec(d Distribution, spec javaversion.Spec) error {
	if ea, ok := d.(earlyAccess); spec.EA() && (!ok || !ea.EarlyAccess()) {
		return fmt.Errorf("setup-java installs no early-access builds of %s", d.Name())
	}
	return nil
}

// pick returns the newest of builds that satisfies spec.
func pick(spec javaversion.Spec, builds []Release) (Release, error) {
	versions := make([]javaversion.Version, len(builds))
	for i, b := range builds {
		versions[i] = b.Version
	}
	v, err := spec.Resolve(versions)
	if err != nil {
		return Release{}, err
	}
	for _, b := range builds {
		if b.Version == v {
			return b, nil
		}
	}
	return Release{}, errors.New("unreachable: the resolved version is one of the builds")
}

func noBuilds(vendor, pkg string, p Platform, ea bool) string {
	kind := "releases"
	if ea {
		kind = "early-access builds"
	}
	return fmt.Sprintf("%s publishes no %s %s for %s", vendor, pkg, kind, p)
}

// fetchSHA256 reads the checksum file at url, whose first field is the hex
// SHA-256. It returns "" when there is no such file.
func fetchSHA256(ctx context.Context, url string) (string, error) {
	var body string
	err := newClient(ctx, "checksum", "").Call(ctx, hc.NewRequest(http.MethodGet, "%s",
		hc.RouteParams(url),
		hc.StringDecoder(&body),
	))
	if hc.HasStatusCode(err, http.StatusNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("fetch checksum %s: %w", url, err)
	}
	fields := strings.Fields(body)
	if len(fields) == 0 || len(fields[0]) != 64 {
		return "", fmt.Errorf("checksum %s holds no SHA-256", url)
	}
	return strings.ToLower(fields[0]), nil
}

func newClient(ctx context.Context, name, baseURL string) *hc.Client {
	return hc.New(ctx, hc.Config{Name: name, BaseURL: baseURL, Timeout: 2 * time.Minute})
}

// getJSON decodes the JSON at route into out. found is false on a 404.
func getJSON(ctx context.Context, client *hc.Client, route string, params map[string]string, out any) (found bool, err error) {
	err = client.Call(ctx, hc.NewRequest(http.MethodGet, "%s",
		hc.RouteParams(route),
		hc.QueryParams(params),
		hc.JSONDecoder(out),
	))
	if hc.HasStatusCode(err, http.StatusNotFound) {
		return false, nil
	}
	return err == nil, err
}
