// Package buildctx works around a performance problem in `container build`:
// the host-side file sync walks every file in the build context — including
// ones excluded by .dockerignore — at a per-file CPU cost, three times per
// build (apple/container#2026). For contexts with large ignored trees (a Rust
// target/, node_modules, .git, ...) this adds minutes to every build.
//
// Until that is fixed upstream, Prepare copies only the non-ignored files
// into a temporary directory and the build runs from there, so the walk only
// ever sees files that would be sent anyway. On APFS the copy uses clonefile,
// which is fast and consumes no extra disk space.
package buildctx

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/moby/patternmatcher"
	"github.com/moby/patternmatcher/ignorefile"
)

// DisableEnv, when set to a non-empty value, disables context filtering and
// builds run from the original context directory (the pre-workaround
// behavior).
const DisableEnv = "APRICOT_DISABLE_CONTEXT_FILTER"

// Prepare creates a filtered copy of the build context under the user cache
// directory and returns its path plus a cleanup function that removes it.
//
// It returns dir == "" (and a nil cleanup) when filtering would not help or
// cannot be done safely: filtering is disabled via DisableEnv, the context
// has no .dockerignore, or the dockerfile lives outside the context (an
// absolute path apricot never rewrites).
//
// The temporary directory must NOT live under /tmp: `container build` fails
// to read file contents from contexts there (files resolve as "not found"),
// so the copy goes under os.UserCacheDir (~/Library/Caches on macOS), which
// is also normally on the same APFS volume as the source — the condition for
// clonefile to work.
func Prepare(contextDir, dockerfile string) (dir string, cleanup func(), err error) {
	if os.Getenv(DisableEnv) != "" {
		return "", nil, nil
	}
	if filepath.IsAbs(dockerfile) {
		return "", nil, nil
	}

	ign, err := os.Open(filepath.Join(contextDir, ".dockerignore"))
	if err != nil {
		// No .dockerignore means nothing is excluded, so a filtered copy
		// would walk exactly as many files as the original context.
		return "", nil, nil
	}
	patterns, err := ignorefile.ReadAll(ign)
	ign.Close()
	if err != nil {
		return "", nil, fmt.Errorf("read .dockerignore: %w", err)
	}
	if len(patterns) == 0 {
		return "", nil, nil
	}

	// The builder always needs the Dockerfile and .dockerignore themselves,
	// even when a pattern (or an excluded parent directory) matches them.
	// Docker's CLI does the same by appending negation patterns.
	df := dockerfile
	if df == "" {
		df = "Dockerfile"
	}
	patterns = append(patterns, "!.dockerignore", "!"+filepath.ToSlash(filepath.Clean(df)))

	pm, err := patternmatcher.New(patterns)
	if err != nil {
		return "", nil, fmt.Errorf("parse .dockerignore: %w", err)
	}

	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return "", nil, err
	}
	base := filepath.Join(cacheRoot, "apricot", "build-ctx")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", nil, err
	}
	tmp, err := os.MkdirTemp(base, "ctx-*")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { os.RemoveAll(tmp) }

	walkErr := filepath.WalkDir(contextDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(contextDir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		excluded, err := pm.MatchesOrParentMatches(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		dst := filepath.Join(tmp, rel)
		if d.IsDir() {
			if excluded {
				if pm.Exclusions() {
					// A ! pattern may re-include a descendant, so the tree
					// must still be walked; the directory itself is created
					// on demand when a descendant survives.
					return nil
				}
				return filepath.SkipDir
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(dst, info.Mode().Perm())
		}
		if excluded {
			return nil
		}
		return copyEntry(p, dst, d)
	})
	if walkErr != nil {
		cleanup()
		return "", nil, walkErr
	}
	return tmp, cleanup, nil
}

// copyEntry replicates one non-directory context entry, creating missing
// parent directories (needed when an excluded directory has a re-included
// descendant).
func copyEntry(src, dst string, d fs.DirEntry) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if d.Type()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	if !d.Type().IsRegular() {
		// Sockets, devices, FIFOs: not meaningful in a build context.
		return nil
	}
	info, err := d.Info()
	if err != nil {
		return err
	}
	return copyFile(src, dst, info.Mode())
}
