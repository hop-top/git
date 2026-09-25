package hop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
)

// The carry, in the order performConversion runs it:
//
//  1. carryLinkedAdminDirs, right after the clone: copies each admin dir
//     to <hub>/worktrees/<id>, keeping the id, and points its gitdir file
//     at the worktree's final .git. Copied, not moved: the old admin dirs
//     keep the worktrees working until the swap deletes them, and the
//     backup restores them on a rollback either way. A shallow
//     repository is cloned through the transport, which copies only
//     what a ref reaches, so its objects are copied too: an index can
//     name blobs no ref reaches.
//  2. moveNestedLinked, before the main working tree moves: a worktree
//     inside it moves to its hops/ destination instead of riding along.
//  3. The swap, then `git worktree repair <default worktree>`, which
//     first sweeps every admin dir and rewrites each worktree's .git file
//     from the gitdir file step 1 wrote. Carried paths are never passed
//     to repair: for a path, repair guesses the admin dir from the id in
//     a stale .git file, and when that id now names another worktree's
//     admin dir it takes that dir over.
//  4. finishLinkedCarry: reconnects the submodules of each carried
//     worktree, whose git dirs moved one level up with the admin dir,
//     and verifies each present worktree reaches its admin dir in the
//     hub. A failure fails the conversion, which rolls back.

// carryLinkedAdminDirs is step 1.
func (c *Converter) carryLinkedAdminDirs(repoPath, bareRepo string) error {
	if c.linked == nil || len(c.linked.Worktrees) == 0 {
		return nil
	}
	for _, w := range c.linked.Worktrees {
		src := filepath.Join(repoPath, ".git", "worktrees", w.ID)
		dst := filepath.Join(bareRepo, "worktrees", w.ID)
		if err := copyTree(c.fs, src, dst); err != nil {
			return fmt.Errorf("failed to carry the admin dir of linked worktree %s: %w", w.Path, err)
		}
		gitdir := filepath.Join(w.FinalPath(repoPath), ".git") + "\n"
		if err := afero.WriteFile(c.fs, filepath.Join(dst, "gitdir"), []byte(gitdir), 0o644); err != nil {
			return fmt.Errorf("failed to point the admin dir of linked worktree %s at it: %w", w.Path, err)
		}
	}
	if shallow, _ := c.git.Run("git", "-C", repoPath, "rev-parse", "--is-shallow-repository"); shallow == "true" {
		if err := c.copyMissingObjects(filepath.Join(repoPath, ".git", "objects"), filepath.Join(bareRepo, "objects")); err != nil {
			return fmt.Errorf("failed to carry the objects of the linked worktrees' indexes: %w", err)
		}
	}
	return nil
}

// copyMissingObjects copies every loose object and pack under src that
// dst lacks. info/ is left out: alternates, the commit graph and
// packs listing describe src's own store. So is a multi-pack index,
// which would not cover dst's own packs.
func (c *Converter) copyMissingObjects(src, dst string) error {
	return afero.Walk(c.fs, src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return err
		}
		if info.IsDir() {
			if rel == "info" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(info.Name(), "multi-pack-index") {
			return nil
		}
		target := filepath.Join(dst, rel)
		if ok, _ := afero.Exists(c.fs, target); ok {
			return nil
		}
		if err := c.fs.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFileMode(c.fs, path, target)
	})
}

// moveNestedLinked is step 2: each worktree inside the repository's
// working tree moves to <bareRepo>/<Dest>, and the directories that held
// only it are removed, so the default worktree does not inherit them
// empty.
func (c *Converter) moveNestedLinked(repoPath, bareRepo string) error {
	if c.linked == nil {
		return nil
	}
	for _, w := range c.linked.Worktrees {
		if w.Dest == "" {
			continue
		}
		dst := filepath.Join(bareRepo, w.Dest)
		if err := c.fs.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := c.fs.Rename(w.Path, dst); err != nil {
			return fmt.Errorf("failed to move linked worktree %s to %s: %w", w.Path, w.Dest, err)
		}
		for dir := filepath.Dir(w.Path); dir != repoPath && pathWithin(dir, repoPath); dir = filepath.Dir(dir) {
			if c.fs.Remove(dir) != nil {
				break // not empty: it holds more than the worktree
			}
		}
	}
	return nil
}

// finishLinkedCarry is step 4, after the swap and the repair: the hub is
// at repoPath.
func (c *Converter) finishLinkedCarry(repoPath string, result *config.ConversionResult) error {
	if c.linked == nil {
		return nil
	}
	hubReal := c.linked.realRepo
	for _, w := range c.linked.Worktrees {
		final := w.FinalPath(repoPath)
		if !w.Present {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"linked worktree %s is locked and absent; carried as it is. Once it is back, run 'git -C %s worktree repair'",
				w.Path, repoPath))
		} else {
			newModules := filepath.Join(repoPath, "worktrees", w.ID, "modules")
			if ok, _ := afero.DirExists(c.fs, newModules); ok {
				oldModules := filepath.Join(hubReal, ".git", "worktrees", w.ID, "modules")
				newRoot := w.realPath
				if w.Dest != "" {
					newRoot = filepath.Join(hubReal, w.Dest)
				}
				if err := c.reconnectModules(newModules, oldModules, w.realPath, newRoot); err != nil {
					return fmt.Errorf("failed to reconnect the submodules of linked worktree %s: %w", final, err)
				}
			}
			if err := c.verifyLinked(final, filepath.Join(hubReal, "worktrees", w.ID)); err != nil {
				return err
			}
		}
		carried := config.CarriedWorktree{Path: final, Branch: w.Branch}
		if w.Dest != "" {
			carried.MovedFrom = w.Path
		}
		result.Carried = append(result.Carried, carried)
	}
	for _, p := range c.linked.Prunable {
		result.Warnings = append(result.Warnings, fmt.Sprintf(
			"linked worktree %s is prunable (its directory is gone); left behind", p))
	}
	return nil
}

// verifyLinked checks that git, run in the worktree at dir, finds its git
// dir at want.
func (c *Converter) verifyLinked(dir, want string) error {
	got, err := c.git.Run("git", "-C", dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return fmt.Errorf("linked worktree %s does not reach the hub after the conversion: %w", dir, err)
	}
	if realPath(strings.TrimSpace(got)) != realPath(want) {
		return fmt.Errorf("linked worktree %s does not reach the hub after the conversion: its git dir is %s, want %s",
			dir, strings.TrimSpace(got), want)
	}
	return nil
}

// repairLinkedAfterRollback reconnects the linked worktrees of a
// repository just restored from its backup. The restore brings back the
// admin dirs, but the worktrees outside the repository were never in
// the backup: the conversion's repair pointed their .git files at the
// hub. A sweep repair rewrites each from its admin dir's gitdir file.
func (c *Converter) repairLinkedAfterRollback(repoPath string) error {
	if c.linked == nil || len(c.linked.Worktrees) == 0 {
		return nil
	}
	if _, err := c.git.Run("git", "-C", repoPath, "worktree", "repair"); err != nil {
		return fmt.Errorf("failed to reconnect the linked worktrees after the rollback; run 'git -C %s worktree repair': %w", repoPath, err)
	}
	return nil
}

// hopConfigBranches is hop.json's branches: the default branch at
// branchPath, and each carried linked worktree on a branch at its
// hops/ destination or, when it stayed outside the hub, its absolute
// path. A detached worktree has no branch to list it under.
func (c *Converter) hopConfigBranches(repoPath, defaultBranch, branchPath string) map[string]interface{} {
	branches := map[string]interface{}{
		defaultBranch: map[string]interface{}{"path": branchPath, "exists": true},
	}
	if c.linked == nil {
		return branches
	}
	for _, w := range c.linked.Worktrees {
		if w.Branch == "" {
			continue
		}
		path := w.Path
		if w.Dest != "" {
			path = w.Dest
		}
		branches[w.Branch] = map[string]interface{}{"path": path, "exists": true}
	}
	return branches
}

// copyTree copies the directory src to dst, file modes included.
func copyTree(fs afero.Fs, src, dst string) error {
	srcInfo, err := fs.Stat(src)
	if err != nil {
		return err
	}
	if err := fs.MkdirAll(dst, srcInfo.Mode().Perm()); err != nil {
		return err
	}
	// MkdirAll does not chmod when the dir already exists; force parity.
	if err := fs.Chmod(dst, srcInfo.Mode().Perm()); err != nil {
		return err
	}
	entries, err := afero.ReadDir(fs, src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		s, d := filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			err = copyTree(fs, s, d)
		} else {
			err = copyFileMode(fs, s, d)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// copyFileMode copies the file src to dst with its permission bits.
func copyFileMode(fs afero.Fs, src, dst string) error {
	info, err := fs.Stat(src)
	if err != nil {
		return err
	}
	data, err := afero.ReadFile(fs, src)
	if err != nil {
		return err
	}
	if err := afero.WriteFile(fs, dst, data, info.Mode().Perm()); err != nil {
		return err
	}
	return fs.Chmod(dst, info.Mode().Perm())
}
