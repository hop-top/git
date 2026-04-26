package cmd

import (
	"fmt"
	"strings"

	"hop.top/git/internal/git"
)

// branchSafety captures the three signals that decide whether `git hop
// remove` may proceed without --force / --no-verify:
//   - Merged: branch tip is reachable from defaultBranch
//   - Pushed: branch tip is reachable from origin/<branch>
//   - Clean:  no uncommitted changes or untracked files in the worktree
//
// A field is true only when we proved the positive. On any git error
// or missing ref, we set the field false so the gate fails closed.
type branchSafety struct {
	Merged bool
	Pushed bool
	Clean  bool
}

// inspectBranchSafety probes the worktree at dir to populate
// branchSafety. dir must be a real worktree on disk; defaultBranch is
// the hub's default and may be empty (in which case Merged is reported
// false, since we can't compare).
func inspectBranchSafety(g git.GitInterface, dir, branch, defaultBranch string) branchSafety {
	s := branchSafety{}

	if defaultBranch != "" && branch != defaultBranch {
		// Branch is merged into default when it has no commits ahead of
		// default. Use rev-list --count <branch> --not <default>.
		out, err := g.RunInDir(dir, "git", "rev-list", "--count", branch, "--not", defaultBranch)
		if err == nil && strings.TrimSpace(out) == "0" {
			s.Merged = true
		}
	}

	// Pushed: origin/<branch> exists AND branch has no commits ahead of it.
	if _, err := g.RunInDir(dir, "git", "rev-parse", "--verify", "refs/remotes/origin/"+branch); err == nil {
		out, err := g.RunInDir(dir, "git", "rev-list", "--count", branch, "--not", "refs/remotes/origin/"+branch)
		if err == nil && strings.TrimSpace(out) == "0" {
			s.Pushed = true
		}
	}

	// Clean: status --porcelain output is empty.
	if status, err := g.GetStatus(dir); err == nil {
		s.Clean = status.Clean
	}

	return s
}

// removeGate decides whether the remove can proceed and returns a
// human-readable error explaining what the user must do.
//
// Matrix (per spec):
//
//	merged | pushed | dirty | requires
//	-------|--------|-------|----------------------
//	  no   |   no   |  any  | --force --no-verify
//	  no   |  yes   |  any  | --force
//	 yes   |  any   | dirty | --no-verify
//	 yes   |  any   | clean | (silent pass)
func removeGate(s branchSafety, force, noVerify bool) error {
	dirty := !s.Clean

	switch {
	case !s.Merged && !s.Pushed:
		if !force || !noVerify {
			return fmt.Errorf(
				"branch is not merged into default and not pushed to origin; " +
					"pass --force --no-verify to remove it anyway",
			)
		}
	case !s.Merged:
		if !force {
			return fmt.Errorf(
				"branch is not merged into default; pass --force to remove it anyway",
			)
		}
	case dirty:
		if !noVerify {
			return fmt.Errorf(
				"worktree has uncommitted changes or untracked files; " +
					"pass --no-verify to remove it anyway",
			)
		}
	}
	return nil
}
