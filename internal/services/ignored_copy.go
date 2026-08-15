// Package services — ignored_copy.go seeds a freshly-created worktree with
// the git-ignored local state that already exists in the worktree it was
// forked from (.env files, editor/tool config, local caches).
//
// The entry list is never hardcoded: git already knows what is ignored, so
// the set is derived from `git status --porcelain --ignored=traditional`.
// Encoding a list of package-manager or tool directories here would
// reintroduce exactly the maintenance burden this feature exists to remove.
package services

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/afero"
)

// StatusRunner is the slice of git this file needs: the ability to run a
// command in a directory. Declaring it here rather than taking the full
// git.GitInterface keeps the dependency honest — this code runs exactly one
// git invocation — and lets tests supply a fake without stubbing forty
// unrelated methods. git.GitInterface satisfies it.
type StatusRunner interface {
	RunInDir(dir string, cmd string, args ...string) (string, error)
}

// DefaultIgnoredCopyMaxBytes is the per-entry size ceiling. Entries larger
// than this are skipped and reported rather than copied.
//
// 10 MiB sits above the local state this feature targets (.env files, tool
// config, editor settings, small caches — kilobytes each) and below the
// build output it must not propagate. The motivating case is a compiled
// binary committed nowhere but sitting in the worktree root: copying tens
// of megabytes into every new worktree is precisely the cost users do not
// expect from `git hop add`.
const DefaultIgnoredCopyMaxBytes int64 = 10 << 20 // 10 MiB

// IgnoredCopyResult reports what a CopyIgnored run did. Copied and Skipped
// are relative paths as reported by git, so they are stable to print.
type IgnoredCopyResult struct {
	// Copied lists entries successfully materialised in the destination.
	Copied []string
	// Skipped lists entries deliberately left behind, each with a reason.
	Skipped []IgnoredSkip
	// Warnings lists non-fatal problems hit while copying. A populated
	// Warnings never means the overall operation failed: a worktree
	// without some of its ignored files beats a failed create.
	Warnings []string
}

// SkipReason classifies why an ignored entry was not copied.
type SkipReason string

const (
	// SkipTooLarge marks an entry above the configured size ceiling.
	SkipTooLarge SkipReason = "too-large"
	// SkipDepsManaged marks a path owned by the deps layer, which
	// symlinks it to a shared store keyed on lockfile hash. Copying it
	// would duplicate gigabytes on disk and fight those symlinks.
	SkipDepsManaged SkipReason = "deps-managed"
	// SkipExists marks a path already present in the destination. The
	// destination is never overwritten.
	SkipExists SkipReason = "exists"
)

// IgnoredSkip is a single skipped entry plus the reason and, for size
// skips, the measured byte count that tripped the ceiling.
type IgnoredSkip struct {
	Path   string
	Reason SkipReason
	Bytes  int64
}

// IgnoredCopyOptions configures a CopyIgnored run.
type IgnoredCopyOptions struct {
	// MaxBytes is the per-entry ceiling. Zero or negative means
	// DefaultIgnoredCopyMaxBytes.
	MaxBytes int64
	// DepsManaged is the set of destination-relative paths the deps layer
	// owns. Supplied by DepsManagedPaths so the list is asked for, never
	// re-derived here.
	DepsManaged map[string]bool
}

// DepsManagedPaths returns the worktree-relative paths that the deps layer
// manages for the given worktree, as a set suitable for
// IgnoredCopyOptions.DepsManaged.
//
// The set is obtained by asking the deps layer itself — DetectInWorktree
// reports which package managers apply, and each contributes its DepsDir
// (node_modules, vendor, venv, target, vendor/bundle, …). Deriving the
// list any other way would let the two drift: a custom package manager
// configured in managers.json would be symlinked by EnsureDeps but copied
// by this function, producing a real directory where a symlink belongs.
//
// Go's vendor/ is included deliberately, and for a reason distinct from
// the others. deps_manager.go documents that git-hop must NEVER touch a Go
// vendor/: it is committed source of truth materialised by `git checkout`,
// not a regenerable cache. If it is tracked, git never reports it as
// ignored and this function never sees it. If it IS gitignored, the user
// has opted out of committing it and manages it by hand — copying another
// worktree's vendor/ on top would be git-hop mutating a directory it has
// no claim to. Either way, skipping is correct.
func DepsManagedPaths(m *DepsManager, worktreePath string) map[string]bool {
	managed := make(map[string]bool)
	if m == nil {
		return managed
	}
	detected, err := m.DetectInWorktree(worktreePath)
	if err != nil {
		// Detection failure means we cannot prove a path is unmanaged.
		// Return empty rather than a partial set; the caller treats an
		// empty set as "nothing known to be managed" and the no-overwrite
		// guard still protects any symlink already in place.
		return managed
	}
	for _, pm := range detected {
		if pm.DepsDir == "" {
			continue
		}
		managed[filepath.ToSlash(filepath.Clean(pm.DepsDir))] = true
	}
	return managed
}

// ListIgnoredEntries returns the ignored-but-present entries of the
// worktree at path, as worktree-relative paths with any trailing slash
// stripped.
//
// It uses `--ignored=traditional` rather than `--ignored=matching`. The two
// differ on a directory whose every entry is ignored but whose own name
// matches no ignore pattern: `matching` recurses and enumerates each file
// inside, `traditional` collapses it to the single directory entry. That
// distinction decides both cost and correctness here:
//
//   - Cost: a cache directory with 50k files yields 50k lines under
//     `matching` and one under `traditional`. `git hop add` must not pay a
//     per-file price to enumerate directories it is about to copy wholesale.
//   - Correctness: the size guard and the deps-managed guard both apply to
//     a directory as a unit. Given per-file entries, a 24 MiB cache arrives
//     as thousands of individually-under-threshold files and slips past the
//     ceiling entirely, and a deps-managed node_modules/ arrives as
//     node_modules/x/y.js paths that no longer prefix-match the managed
//     name. Collapsed directory entries keep both guards decidable.
//
// -unormal (git's default) is passed explicitly: -uall would force the
// per-file expansion that traditional exists to avoid.
func ListIgnoredEntries(g StatusRunner, worktreePath string) ([]string, error) {
	out, err := g.RunInDir(worktreePath, "git", "status", "--porcelain",
		"--ignored=traditional", "-unormal")
	if err != nil {
		return nil, fmt.Errorf("failed to list ignored entries: %w", err)
	}

	var entries []string
	for _, line := range strings.Split(out, "\n") {
		// Porcelain v1 ignored lines are exactly `!! <path>`. Anything
		// else is a tracked/untracked status line and not ours.
		if !strings.HasPrefix(line, "!! ") {
			continue
		}
		p := strings.TrimSpace(strings.TrimPrefix(line, "!! "))
		// git quotes paths containing special characters. Skipping them
		// is safer than half-unquoting: a mis-parsed path would copy the
		// wrong file, and these are rare in the local-state this targets.
		if p == "" || strings.HasPrefix(p, `"`) {
			continue
		}
		p = strings.TrimSuffix(p, "/")
		// Defence against a path escaping the worktree. git never emits
		// one, but this list drives filesystem writes into a new worktree.
		if filepath.IsAbs(p) || p == ".." || strings.HasPrefix(p, "../") {
			continue
		}
		entries = append(entries, p)
	}
	sort.Strings(entries)
	return entries, nil
}

// CopyIgnored copies the ignored-but-present entries of srcWorktree into
// dstWorktree, honouring the size ceiling, the deps-managed skip list, and
// a strict no-overwrite rule.
//
// It never returns an error for a per-entry failure: those land in
// Warnings and the run continues. A non-nil error means the entry list
// itself could not be obtained, which the caller likewise treats as
// non-fatal.
func CopyIgnored(fs afero.Fs, g StatusRunner, srcWorktree, dstWorktree string, opts IgnoredCopyOptions) (*IgnoredCopyResult, error) {
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultIgnoredCopyMaxBytes
	}

	res := &IgnoredCopyResult{}

	entries, err := ListIgnoredEntries(g, srcWorktree)
	if err != nil {
		return res, err
	}

	for _, rel := range entries {
		if opts.DepsManaged[filepath.ToSlash(rel)] {
			res.Skipped = append(res.Skipped, IgnoredSkip{Path: rel, Reason: SkipDepsManaged})
			continue
		}

		srcPath := filepath.Join(srcWorktree, rel)
		dstPath := filepath.Join(dstWorktree, rel)

		// Never overwrite. LstatIfPossible is used so an existing symlink
		// in the destination (a deps-layer symlink already materialised)
		// counts as present rather than being followed to a missing target.
		if exists, err := lexists(fs, dstPath); err != nil {
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("could not check %s in destination: %v", rel, err))
			continue
		} else if exists {
			res.Skipped = append(res.Skipped, IgnoredSkip{Path: rel, Reason: SkipExists})
			continue
		}

		info, err := lstat(fs, srcPath)
		if err != nil {
			// Raced away between the status call and now, or unreadable.
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("could not stat %s: %v", rel, err))
			continue
		}

		// A symlink is copied as a symlink — never followed. Following
		// would risk pulling in an unbounded tree (or a cycle) through a
		// link that points at a parent directory.
		if info.Mode()&os.ModeSymlink != 0 {
			if err := copySymlink(fs, srcPath, dstPath); err != nil {
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("could not copy symlink %s: %v", rel, err))
				continue
			}
			res.Copied = append(res.Copied, rel)
			continue
		}

		size, err := entrySize(fs, srcPath, info, maxBytes)
		if err != nil {
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("could not size %s: %v", rel, err))
			continue
		}
		if size > maxBytes {
			res.Skipped = append(res.Skipped,
				IgnoredSkip{Path: rel, Reason: SkipTooLarge, Bytes: size})
			continue
		}

		if err := fs.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("could not create parent for %s: %v", rel, err))
			continue
		}

		if info.IsDir() {
			err = copyIgnoredDir(fs, srcPath, dstPath, info.Mode().Perm())
		} else {
			err = copyIgnoredFile(fs, srcPath, dstPath, info.Mode().Perm())
		}
		if err != nil {
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("could not copy %s: %v", rel, err))
			continue
		}
		res.Copied = append(res.Copied, rel)
	}

	return res, nil
}

// entrySize returns the byte size of a file, or the recursive size of a
// directory. Directory walking stops early once the running total passes
// limit: the caller only needs to know whether the ceiling was crossed, and
// a multi-gigabyte cache should not be fully traversed to learn that.
// Symlinks encountered inside contribute nothing and are not followed.
func entrySize(fs afero.Fs, path string, info os.FileInfo, limit int64) (int64, error) {
	if !info.IsDir() {
		return info.Size(), nil
	}
	var total int64
	errStop := fmt.Errorf("size limit exceeded")
	err := afero.Walk(fs, path, func(_ string, fi os.FileInfo, err error) error {
		if err != nil {
			// An unreadable child should not abort the whole measurement;
			// it simply contributes nothing.
			return nil
		}
		if fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		total += fi.Size()
		if total > limit {
			return errStop
		}
		return nil
	})
	if err != nil && err.Error() != errStop.Error() {
		return total, err
	}
	return total, nil
}

// copyIgnoredDir recursively copies src to dst preserving permission bits
// on directories and files, and reproducing symlinks as symlinks. It never
// overwrites an existing destination entry.
func copyIgnoredDir(fs afero.Fs, src, dst string, perm os.FileMode) error {
	if err := fs.MkdirAll(dst, perm); err != nil {
		return err
	}
	// MkdirAll does not chmod an existing directory; force parity so the
	// copied tree keeps the source's access bits.
	if err := fs.Chmod(dst, perm); err != nil {
		return err
	}

	entries, err := afero.ReadDir(fs, src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())

		if exists, err := lexists(fs, dstPath); err == nil && exists {
			continue
		}

		info, err := lstat(fs, srcPath)
		if err != nil {
			continue
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			if err := copySymlink(fs, srcPath, dstPath); err != nil {
				return err
			}
		case info.IsDir():
			if err := copyIgnoredDir(fs, srcPath, dstPath, info.Mode().Perm()); err != nil {
				return err
			}
		default:
			if err := copyIgnoredFile(fs, srcPath, dstPath, info.Mode().Perm()); err != nil {
				return err
			}
		}
	}
	return nil
}

// copyIgnoredFile copies a single regular file preserving its permission
// bits.
//
// The explicit Chmod after the write is load-bearing: afero.WriteFile
// honours mode only when it creates the file, so a write over an existing
// path silently keeps the old mode and drops the exec bit. internal/hop
// backup.go carries the same fix for the same reason — restoring a backup
// over an existing tree stripped the exec bit off every .sh script.
func copyIgnoredFile(fs afero.Fs, src, dst string, perm os.FileMode) error {
	data, err := afero.ReadFile(fs, src)
	if err != nil {
		return err
	}
	if err := afero.WriteFile(fs, dst, data, perm); err != nil {
		return err
	}
	return fs.Chmod(dst, perm)
}

// copySymlink reproduces src's link target at dst without following it.
// On a filesystem without symlink support (afero.MemMapFs) this is a no-op
// rather than an error, so tree copies on such backends still succeed.
func copySymlink(fs afero.Fs, src, dst string) error {
	linker, ok := fs.(afero.Symlinker)
	if !ok {
		return nil
	}
	target, err := linker.ReadlinkIfPossible(src)
	if err != nil {
		return err
	}
	return linker.SymlinkIfPossible(target, dst)
}

// lstat stats path without following a final symlink, falling back to a
// following Stat on filesystems that cannot lstat.
func lstat(fs afero.Fs, path string) (os.FileInfo, error) {
	if lstater, ok := fs.(afero.Lstater); ok {
		info, done, err := lstater.LstatIfPossible(path)
		if done || err == nil {
			return info, err
		}
	}
	return fs.Stat(path)
}

// lexists reports whether path exists, treating a dangling symlink as
// existing (it occupies the name, so writing there would overwrite it).
func lexists(fs afero.Fs, path string) (bool, error) {
	if _, err := lstat(fs, path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
