package hop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

// carryOverSubmodules moves .git/modules, the git dirs of the
// repository's submodules, to <worktreeGitDir>/modules: git keeps a
// linked worktree's submodules in that worktree's own git dir, and it is
// where `git submodule update` looks for them. It then reconnects each
// module with its checkout under worktreePath, the two links git writes
// when it absorbs a submodule: core.worktree in the module's config and
// the gitfile in the checkout. Both are relative, as git writes them, so
// they survive the hub's swap into place.
//
// Git has no command for this move. `git submodule absorbgitdirs` only
// moves a git dir embedded in a checkout, and `git worktree move`
// refuses a worktree with submodules.
//
// Nested submodules live in their parent module's modules/ dir, which
// moves with it. Their working trees sit inside the superproject's, so
// the same old-root to new-root mapping reconnects them at any depth.
// Submodules whose git dir is embedded in the checkout (.git is a dir)
// have no entry here and move with the working tree.
func (c *Converter) carryOverSubmodules(repoPath, worktreePath, worktreeGitDir string) error {
	oldRoot := realPath(repoPath)
	oldModules := filepath.Join(oldRoot, ".git", "modules")
	if _, err := c.fs.Stat(oldModules); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	newModules := filepath.Join(realPath(worktreeGitDir), "modules")
	if err := c.moveIfExists(oldModules, newModules); err != nil {
		return fmt.Errorf("failed to move .git/modules: %w", err)
	}
	return c.reconnectModules(newModules, oldModules, oldRoot, realPath(worktreePath))
}

// realPath resolves symlinks in path, so paths git reports (which it
// resolves) and paths the caller gave compare and relate; path as given
// if it cannot be resolved.
func realPath(path string) string {
	if p, err := filepath.EvalSymlinks(path); err == nil {
		return p
	}
	return path
}

// reconnectModules walks a modules/ dir. A module's name can hold
// slashes, so a dir that is not a git dir is walked into; a git dir is
// reconnected, then its own modules/ dir walked.
func (c *Converter) reconnectModules(newDir, oldDir, oldRoot, newRoot string) error {
	if c.isGitDir(newDir) {
		if err := c.reconnectModule(newDir, oldDir, oldRoot, newRoot); err != nil {
			return err
		}
		newDir, oldDir = filepath.Join(newDir, "modules"), filepath.Join(oldDir, "modules")
	}
	entries, err := afero.ReadDir(c.fs, newDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if err := c.reconnectModules(filepath.Join(newDir, e.Name()), filepath.Join(oldDir, e.Name()), oldRoot, newRoot); err != nil {
			return err
		}
	}
	return nil
}

func (c *Converter) isGitDir(dir string) bool {
	for _, name := range []string{"HEAD", "config"} {
		if info, err := c.fs.Stat(filepath.Join(dir, name)); err != nil || info.IsDir() {
			return false
		}
	}
	return true
}

// reconnectModule points the module at newDir, which was at oldDir, to
// its working tree's new place, and that working tree's gitfile back at
// newDir. A module with no core.worktree (never checked out) or one
// outside the old repository is left as it is.
func (c *Converter) reconnectModule(newDir, oldDir, oldRoot, newRoot string) error {
	cfg := filepath.Join(newDir, "config")
	wt, err := c.git.Run("git", "config", "--file", cfg, "--default", "", "core.worktree")
	if err != nil {
		return fmt.Errorf("failed to read core.worktree of %s: %w", newDir, err)
	}
	if wt == "" {
		return nil
	}
	oldWT := wt
	if !filepath.IsAbs(wt) {
		oldWT = filepath.Join(oldDir, wt)
	}
	rel, err := filepath.Rel(oldRoot, oldWT)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}
	newWT := filepath.Join(newRoot, rel)

	relWT, err := filepath.Rel(newDir, newWT)
	if err != nil {
		return err
	}
	if _, err := c.git.Run("git", "config", "--file", cfg, "core.worktree", relWT); err != nil {
		return fmt.Errorf("failed to set core.worktree of %s: %w", newDir, err)
	}

	gitfile := filepath.Join(newWT, ".git")
	if info, err := c.fs.Stat(gitfile); err != nil || !info.Mode().IsRegular() {
		return nil
	}
	relGit, err := filepath.Rel(newWT, newDir)
	if err != nil {
		return err
	}
	if err := afero.WriteFile(c.fs, gitfile, []byte("gitdir: "+relGit+"\n"), 0o644); err != nil {
		return fmt.Errorf("failed to point %s at %s: %w", gitfile, newDir, err)
	}
	return nil
}
