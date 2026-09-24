package hop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hop.top/git/internal/git"
)

// Per-worktree config (extensions.worktreeConfig) in a bare conversion.
//
// With the extension on, git reads <git dir>/config.worktree on top of the
// shared config, per worktree; `git sparse-checkout` turns it on and keeps
// core.sparseCheckout there. Before the conversion the repository's
// .git/config.worktree belongs to its only worktree, which becomes the
// hub's default worktree, so its entries go to that worktree's own
// <hub>/worktrees/<name>/config.worktree.
//
// The hub gets the extension too, and git's documented requirement
// with it (git-worktree(1), CONFIGURATION FILE): core.bare=true moves
// from the shared config into the hub's own config.worktree. Left in the
// shared config it would apply to every linked worktree, and each would
// fail with "must be run in a work tree".
//
// A repository without the extension is converted as before: git was not
// reading a config.worktree file it may have, so it is left behind with
// a warning.

// readWorktreeConfig lists repoPath's .git/config.worktree entries, in
// file order, includes not followed. A missing file has no entries.
func readWorktreeConfig(g git.GitInterface, repoPath string) ([]configEntry, error) {
	path := filepath.Join(repoPath, ".git", "config.worktree")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	out, err := g.Run("git", "config", "--file", path, "--no-includes", "--null", "--list")
	if err != nil {
		return nil, err
	}
	return parseConfigList(out), nil
}

// carryOverWorktreeConfig applies the plan's per-worktree part to the hub
// at bareRepo and the default worktree (checkout worktreePath, git dir
// worktreeGitDir). Runs after carryOverGitDir, which has put
// info/sparse-checkout in the worktree's git dir.
func (c *Converter) carryOverWorktreeConfig(plan *LocalConfigPlan, repoPath, bareRepo, worktreePath, worktreeGitDir string) ([]string, error) {
	if !plan.WorktreeConfig {
		if _, err := c.fs.Stat(filepath.Join(repoPath, ".git", "config.worktree")); err == nil {
			return []string{".git/config.worktree: not carried over: extensions.worktreeConfig is off, " +
				"so git was not reading it"}, nil
		}
		return nil, nil
	}

	shared := filepath.Join(bareRepo, "config")
	own := filepath.Join(bareRepo, "config.worktree")
	for _, args := range [][]string{
		{"config", "--file", own, "core.bare", "true"},
		{"config", "--file", shared, "extensions.worktreeConfig", "true"},
		{"config", "--file", shared, "--unset-all", "core.bare"},
	} {
		if _, err := c.git.Run("git", args...); err != nil {
			return nil, fmt.Errorf("failed to move core.bare into the hub's config.worktree: %w", err)
		}
	}

	wtConfig := []string{"config", "--file", filepath.Join(worktreeGitDir, "config.worktree")}
	for _, e := range plan.PerWorktree {
		if err := addConfigEntry(c.git, wtConfig, e); err != nil {
			return nil, err
		}
	}

	// The checkout was made before the sparse settings arrived, so it is
	// full; applying them again removes what the repository did not have.
	if sparse, err := c.git.Run("git", "-C", worktreePath, "config", "--type=bool", "core.sparseCheckout"); err == nil && sparse == "true" {
		if _, err := c.git.Run("git", "-C", worktreePath, "sparse-checkout", "reapply"); err != nil {
			return []string{fmt.Sprintf("sparse checkout not applied to the %s worktree; run 'git sparse-checkout reapply' there: %v",
				filepath.Base(worktreePath), err)}, nil
		}
	}
	return nil, nil
}

// parseConfigList splits `git config --null --list` output into entries.
func parseConfigList(out string) []configEntry {
	var entries []configEntry
	for _, rec := range strings.Split(out, "\x00") {
		if rec == "" {
			continue
		}
		key, value, hasValue := strings.Cut(rec, "\n")
		entries = append(entries, configEntry{key: key, value: value, implicit: !hasValue})
	}
	return entries
}
