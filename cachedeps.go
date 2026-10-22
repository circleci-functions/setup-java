package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/circleci-functions/toolkit/sdk"

	"github.com/circleci-functions/setup-java/depcache"
)

// cacheDepsCmd caches what a build tool downloads, whatever Java the job
// runs, for images that already have one.
type cacheDepsCmd struct {
	BuildTool     string `help:"Build tool whose downloads to cache: ${build_tool_list}." enum:"${build_tools}" required:"" example:"maven"`
	depCacheFlags `embed:""`
	CacheNonce    string `help:"Partition the dependency cache, so a test run cannot restore an earlier run's cache." hidden:""`
}

func (c *cacheDepsCmd) Run(ctx context.Context, client *sdk.Client) error {
	entries, err := c.planDeps(c.BuildTool, c.CacheNonce)
	if err != nil {
		return err
	}
	c.restoreDeps(ctx, client, entries)
	return nil
}

// depCacheFlags shape dependency caching, for both the install and
// cache-dependencies commands.
type depCacheFlags struct {
	CacheDependencyPath []string `help:"Build files to key the dependency cache on, as globs relative to the working directory, instead of the build tool's own. Separate several with commas."`
	CachePath           []string `help:"Folders to cache instead of the build tool's own. Separate several with commas."`
	CacheReadOnly       bool     `help:"Restore the dependency cache, but do not save it at the end of the job."`
}

func (f depCacheFlags) set() bool {
	return len(f.CacheDependencyPath) > 0 || len(f.CachePath) > 0 || f.CacheReadOnly
}

// planDeps works out the caches for tool in the working directory.
func (f depCacheFlags) planDeps(tool, nonce string) ([]depcache.Entry, error) {
	t, _ := depcache.Lookup(tool)
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	entries, err := depcache.Plan(t, depcache.Options{
		ProjectDir: wd,
		Files:      f.CacheDependencyPath,
		Dirs:       f.CachePath,
		Platform:   sdk.OSArch(),
		Nonce:      nonce,
	})
	if errors.Is(err, depcache.ErrNoBuildFiles) {
		return nil, fmt.Errorf("%w; run in the project's directory, or set --cache-dependency-path", err)
	}
	return entries, err
}

// restoreDeps restores the caches, and unless --cache-read-only registers their
// saves for the end of the job.
func (f depCacheFlags) restoreDeps(ctx context.Context, client *sdk.Client, entries []depcache.Entry) {
	depcache.Restore(ctx, client, entries, f.CacheReadOnly)
	for _, e := range entries {
		if f.CacheReadOnly {
			fmt.Fprintf(os.Stderr, "setup-java: restored %s if cached under %s; --cache-read-only, so it is not saved\n", e.Label, e.Key)
			continue
		}
		fmt.Fprintf(os.Stderr, "setup-java: restored %s if cached under %s, and saves it there when the job succeeds\n", e.Label, e.Key)
	}
}
