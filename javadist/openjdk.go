package javadist

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	hc "github.com/circleci-functions/toolkit/httpcl"

	"github.com/circleci-functions/setup-java/javaversion"
)

// JDKJavaNetURL is where Oracle publishes its OpenJDK builds. It has no API:
// each release has a page of download links, and older releases are on its
// archive page.
const JDKJavaNetURL = "https://jdk.java.net"

// OracleOpenJDK is Oracle's builds of OpenJDK, from jdk.java.net.
type OracleOpenJDK struct {
	BaseURL string
}

func (OracleOpenJDK) Name() string { return "oracle-openjdk" }

func (OracleOpenJDK) Packages() []string { return []string{"jdk"} }

var (
	releasePage = regexp.MustCompile(`href="/(\d+)/">JDK\s+\d+`)
	// openjdkLink is a download link, e.g.
	// https://download.java.net/java/GA/jdk21.0.2/<hash>/13/GPL/openjdk-21.0.2_linux-x64_bin.tar.gz,
	// with the build number as the directory before the file, when there is one.
	openjdkLink = regexp.MustCompile(`href="(https?://[^"]+?/(?:(\d+)/(?:GPL/)?)?openjdk-([0-9.]+)_(linux|macos|osx|windows)-(x64|aarch64)_bin\.(tar\.gz|zip))"`)
)

func (o OracleOpenJDK) Resolve(ctx context.Context, spec javaversion.Spec, p Platform, pkg string) (Release, error) {
	client := newClient(ctx, "jdk-java-net", o.BaseURL)
	home, err := o.page(ctx, client, "/")
	if err != nil {
		return Release{}, err
	}
	pages := []string{"/archive/"}
	for _, m := range releasePage.FindAllStringSubmatch(home, -1) {
		pages = append(pages, "/"+m[1]+"/")
	}

	var builds []Release
	for _, route := range pages {
		html, err := o.page(ctx, client, route)
		if err != nil {
			return Release{}, err
		}
		builds = append(builds, openjdkBuilds(html, p)...)
	}
	if len(builds) == 0 {
		return Release{}, errors.New(noBuilds("Oracle OpenJDK", pkg, p, false))
	}
	r, err := pick(spec, builds)
	if err != nil {
		return Release{}, fmt.Errorf("%w in Oracle OpenJDK's %s builds for %s", err, pkg, p)
	}
	if r.SHA256, err = fetchSHA256(ctx, r.URL+".sha256"); err != nil {
		return Release{}, err
	}
	return r, nil
}

func (OracleOpenJDK) page(ctx context.Context, client *hc.Client, route string) (string, error) {
	var html string
	if err := client.Call(ctx, hc.NewRequest(http.MethodGet, "%s", hc.RouteParams(route), hc.StringDecoder(&html))); err != nil {
		return "", fmt.Errorf("list Oracle OpenJDK builds: %w", err)
	}
	return html, nil
}

// openjdkBuilds returns the release builds for p linked from a jdk.java.net
// page. Early-access links are left out.
func openjdkBuilds(html string, p Platform) []Release {
	if p.Libc == "musl" {
		return nil
	}
	var builds []Release
	for _, m := range openjdkLink.FindAllStringSubmatch(html, -1) {
		url, build, version, osName, arch, ext := m[1], m[2], m[3], m[4], m[5], m[6]
		if strings.Contains(url, "/early_access/") || arch != p.Arch || "."+ext != p.ArchiveExt() || openjdkOS(osName) != p.OS {
			continue
		}
		v, err := javaversion.ParseVersion(version)
		if err != nil {
			continue
		}
		v.Build, _ = strconv.Atoi(build)
		builds = append(builds, Release{Version: v, URL: url})
	}
	return builds
}

func openjdkOS(name string) string {
	if name == "osx" {
		return "macos"
	}
	return name
}
