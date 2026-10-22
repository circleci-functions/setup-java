package depcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/circleci-functions/toolkit/sdk"
)

// Options configures Plan.
type Options struct {
	// ProjectDir is where the build files are found.
	ProjectDir string
	// Files replaces the main cache's build file patterns.
	Files []string
	// Dirs replaces the main cache's folders. A leading ~ is the home
	// directory; a relative folder is relative to ProjectDir.
	Dirs []string
	// Platform keeps caches from different platforms apart, e.g. linux-amd64.
	Platform string
	// Nonce, when set, partitions the keys, so a test run cannot restore an
	// earlier run's cache.
	Nonce string
}

// Entry is a cache ready to restore and save.
type Entry struct {
	Label string
	// Key is what the cache is saved under.
	Key string
	// RestoreKeys are tried in order: the exact key, then any cache of the
	// same kind, so a change to the build files only downloads what changed.
	RestoreKeys []string
	Paths       []string
}

// ErrNoBuildFiles is returned when no build file the main cache is keyed on
// exists.
var ErrNoBuildFiles = errors.New("no build files found")

// Plan works out the caches t needs for the project in opts.ProjectDir.
func Plan(t Tool, opts Options) ([]Entry, error) {
	home, err := t.Home()
	if err != nil {
		return nil, err
	}
	files, err := projectFiles(opts.ProjectDir)
	if err != nil {
		return nil, err
	}

	var entries []Entry
	for i, c := range t.Caches {
		patterns, dirs := c.Files, make([]string, len(c.Dirs))
		for j, d := range c.Dirs {
			dirs[j] = filepath.Join(home, d)
		}
		if i == 0 && len(opts.Files) > 0 {
			patterns = opts.Files
		}
		if i == 0 && len(opts.Dirs) > 0 {
			if dirs, err = resolveDirs(opts.Dirs, opts.ProjectDir); err != nil {
				return nil, err
			}
		}

		sum, err := hashFiles(opts.ProjectDir, files, patterns)
		if errors.Is(err, ErrNoBuildFiles) && c.Optional {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%w for %s in %s: looked for %s", err, t.Name, opts.ProjectDir, strings.Join(patterns, ", "))
		}
		prefix := fmt.Sprintf("setup-java-v1-%s-%s-", c.ID, opts.Platform)
		if opts.Nonce != "" {
			prefix += opts.Nonce + "-"
		}
		entries = append(entries, Entry{
			Label:       c.Label,
			Key:         prefix + sum,
			RestoreKeys: []string{prefix + sum, prefix},
			Paths:       dirs,
		})
	}
	return entries, nil
}

// projectFiles lists the files under dir, as slash-separated paths relative
// to it, leaving out version-control metadata.
func projectFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.Type().IsRegular() {
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	slices.Sort(files)
	return files, err
}

// hashFiles returns a hex SHA-256 over the names and contents of the files
// matching any of patterns, or ErrNoBuildFiles when none does.
func hashFiles(dir string, files, patterns []string) (string, error) {
	h := sha256.New()
	matched := 0
	for _, f := range files {
		if !slices.ContainsFunc(patterns, func(p string) bool { return doublestar.MatchUnvalidated(filepath.ToSlash(p), f) }) {
			continue
		}
		if err := hashFile(h, dir, f); err != nil {
			return "", err
		}
		matched++
	}
	if matched == 0 {
		return "", ErrNoBuildFiles
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashFile(w io.Writer, dir, name string) error {
	f, err := os.Open(filepath.Join(dir, filepath.FromSlash(name))) //nolint:gosec // a file in the project being built
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	content := sha256.New()
	if _, err := io.Copy(content, f); err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\x00%x\n", name, content.Sum(nil))
	return err
}

func resolveDirs(dirs []string, projectDir string) ([]string, error) {
	out := make([]string, len(dirs))
	for i, d := range dirs {
		switch {
		case d == "~" || strings.HasPrefix(d, "~/"):
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			out[i] = filepath.Join(home, strings.TrimPrefix(d, "~"))
		case filepath.IsAbs(d):
			out[i] = d
		default:
			out[i] = filepath.Join(projectDir, d)
		}
	}
	return out, nil
}

// Restore restores each entry's cache, and unless readOnly, registers its
// save for the end of the job, should the job succeed. Cache failures are
// logged, not returned: a job runs the same without its cache, only slower.
func Restore(ctx context.Context, client *sdk.Client, entries []Entry, readOnly bool) {
	for _, e := range entries {
		_, err := client.RestoreCache(ctx, e.RestoreKeys, sdk.WithStepName("Restoring "+e.Label))
		sdk.LogCacheErr("restore "+e.Label, err)
		if readOnly {
			continue
		}
		err = client.AtTeardown().OnSuccess().SaveCache(ctx, e.Key, e.Paths, sdk.WithStepName("Saving "+e.Label))
		sdk.LogCacheErr("save "+e.Label, err)
	}
}
