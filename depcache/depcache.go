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
	"path"
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
	// Files replaces the main cache's build file patterns. They may start with
	// ./, or be absolute paths under ProjectDir.
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
	userPatterns, err := projectPatterns(opts.Files, opts.ProjectDir)
	if err != nil {
		return nil, err
	}

	var entries []Entry
	for i, c := range t.Caches {
		patterns, dirs := c.Files, make([]string, len(c.Dirs))
		for j, d := range c.Dirs {
			dirs[j] = filepath.Join(home, d)
		}
		if i == 0 && len(userPatterns) > 0 {
			patterns = userPatterns
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
		// A cache restores to the absolute paths it was saved from, so the
		// prefix carries a hash of them: executors whose $HOME differs must not
		// share a cache that restores where only one of them looks.
		prefix := fmt.Sprintf("setup-java-v1-%s-%s-%s-", c.ID, opts.Platform, dirsHash(dirs))
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

// dirsHash returns a short hex hash of dirs, for a cache key.
func dirsHash(dirs []string) string {
	sum := sha256.Sum256([]byte(strings.Join(dirs, "\x00")))
	return hex.EncodeToString(sum[:4])
}

// projectPatterns turns build file patterns into the slash-separated form,
// relative to projectDir, that projectFiles lists files in: ./ is dropped, and
// an absolute pattern is made relative to projectDir. A pattern outside
// projectDir is an error, since no file it matches is listed.
func projectPatterns(patterns []string, projectDir string) ([]string, error) {
	out := make([]string, len(patterns))
	for i, p := range patterns {
		if filepath.IsAbs(p) {
			rel, err := relativeTo(p, projectDir)
			if err != nil {
				return nil, err
			}
			p = rel
		}
		out[i] = path.Clean(filepath.ToSlash(p))
	}
	return out, nil
}

// relativeTo makes the absolute pattern p relative to dir. Symlinks in the
// part of p before its first glob, and in dir, are resolved first, so that
// /tmp/x and /private/tmp/x, the same directory on macOS, compare equal.
func relativeTo(p, dir string) (string, error) {
	base, rest := doublestar.SplitPattern(filepath.ToSlash(p))
	rel, err := filepath.Rel(resolve(dir), resolve(filepath.FromSlash(base)))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("--cache-dependency-path %s is not under the working directory %s", p, dir)
	}
	return path.Join(filepath.ToSlash(rel), rest), nil
}

// resolve returns p with its symlinks resolved, or p itself if it cannot be.
func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
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
