package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
	"hop.top/git/internal/state"
)

// git's worktree lock is the one answer doctor and prune both give to "is
// this missing worktree really gone?". `git worktree lock` marks a
// worktree whose directory may be unavailable for a while (a drive that
// is not mounted, a network share): git never prunes it and refuses to
// add another worktree at its path. doctor and prune leave it, its state
// entry and its hop.json row alone, and tell the user how to release it.

// worktreeLock reports whether porcelain, the output of `git worktree
// list --porcelain`, marks the worktree at path locked, and the reason
// git recorded for the lock ("" when none was given).
func worktreeLock(porcelain, path string) (reason string, locked bool) {
	return worktreeAttr(porcelain, path, "locked")
}

// gitWorktreeLock reports whether the repository at gitDir has the
// worktree at path locked, and the lock's reason. A registry that cannot
// be read answers not locked.
func gitWorktreeLock(g git.GitInterface, gitDir, path string) (reason string, locked bool) {
	list, err := g.WorktreeListPorcelain(gitDir)
	if err != nil {
		return "", false
	}
	return worktreeLock(list, path)
}

// stateWorktreeLock is gitWorktreeLock for a worktree recorded in state:
// git is asked in the worktree's hub, or the repository's hopspace when
// the hub is gone (findGitDirForRepo). No repository to ask answers not
// locked.
func stateWorktreeLock(fs afero.Fs, g git.GitInterface, repoID string, wt *state.WorktreeState) (reason string, locked bool) {
	gitDir := findGitDirForRepo(fs, repoID, wt.HubPath)
	if gitDir == "" {
		return "", false
	}
	return gitWorktreeLock(g, gitDir, wt.Path)
}

// lockedMissingMessage describes a locked worktree whose directory is
// missing, naming the lock's reason when git has one.
func lockedMissingMessage(path, reason string) string {
	if reason == "" {
		return "worktree directory missing but locked in git: " + path
	}
	return fmt.Sprintf("worktree directory missing but locked in git (%s): %s", reason, path)
}

// unlockHint tells the user how to hand a locked worktree back to
// git-hop's repairs.
func unlockHint(path string) string {
	return fmt.Sprintf("run 'git worktree unlock %s' if it is gone for good and git-hop should handle it", path)
}

// worktreeAttr reports whether porcelain, the output of `git worktree
// list --porcelain`, gives the worktree at path the attribute attr
// ("prunable", "locked"), and the text after it (git's reason), if any.
func worktreeAttr(porcelain, path, attr string) (value string, ok bool) {
	want := resolvedPath(path)
	for _, wt := range parseWorktreeList(porcelain) {
		if resolvedPath(wt.path) != want {
			continue
		}
		if value, ok := wt.attrs[attr]; ok {
			return value, true
		}
	}
	return "", false
}

// registeredWorktree is one record of `git worktree list --porcelain`.
type registeredWorktree struct {
	path string
	// attrs maps each attribute line's first word ("HEAD", "branch",
	// "locked", "prunable", ...) to the rest of the line, "" when there
	// is none.
	attrs map[string]string
}

// branch is the branch checked out in the worktree, "" when none.
func (w registeredWorktree) branch() string {
	return strings.TrimPrefix(w.attrs["branch"], "refs/heads/")
}

// parseWorktreeList splits porcelain, the output of `git worktree list
// --porcelain`, into its records.
func parseWorktreeList(porcelain string) []registeredWorktree {
	var list []registeredWorktree
	var cur *registeredWorktree
	for _, line := range strings.Split(porcelain, "\n") {
		switch {
		case line == "":
			cur = nil
		case strings.HasPrefix(line, "worktree "):
			list = append(list, registeredWorktree{path: strings.TrimPrefix(line, "worktree "), attrs: map[string]string{}})
			cur = &list[len(list)-1]
		case cur != nil:
			key, value, _ := strings.Cut(line, " ")
			cur.attrs[key] = strings.TrimSpace(value)
		}
	}
	return list
}
