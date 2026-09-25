package cmd

import "hop.top/git/internal/git"

// resolveBranchBase normalizes the start-point string fed to
// WorktreeManager into the branch name to persist in HubBranch.Base.
// Returns "" when the base should NOT be recorded:
//   - empty / "default-branch" sentinel (= hub default; fallback handles it)
//   - "initial" sentinel (root commit, not a branch)
//   - input equal to the hub default branch (redundant with fallback)
//   - input that doesn't resolve to a local or remote-tracking branch ref
//     (raw SHA, tag, or a name that no longer exists)
//
// When the input resolves only via `refs/remotes/origin/<name>`, the
// returned base is the bare branch name (no `origin/` prefix) — that's
// the form used for comparison everywhere else.
func resolveBranchBase(g git.GitInterface, worktreePath, startPoint, defaultBranch string) string {
	switch startPoint {
	case "", "default-branch", "initial":
		return ""
	}
	if startPoint == defaultBranch {
		return ""
	}
	if _, err := g.RevParse(worktreePath, "--verify", "refs/heads/"+startPoint); err == nil {
		return startPoint
	}
	if _, err := g.RevParse(worktreePath, "--verify", "refs/remotes/origin/"+startPoint); err == nil {
		return startPoint
	}
	return ""
}

// resolveAddStartPoint applies the configured precedence to pick the
// start-point string passed to WorktreeManager. Empty inputs are skipped.
// The returned value is fed verbatim into WorktreeManager, which decides
// the final ref/SHA based on its own resolution rules (see
// worktree.go:resolveStartPoint). An empty return is interpreted by the
// manager as the built-in default ("default-branch").
func resolveAddStartPoint(flagVal, envVal, configVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if envVal != "" {
		return envVal
	}
	if configVal != "" {
		return configVal
	}
	return ""
}
