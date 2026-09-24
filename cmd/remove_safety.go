package cmd

import (
	"fmt"
	"strings"

	"hop.top/git/internal/git"
)

// branchSafety captures the three signals that decide whether `git hop
// remove` may proceed without --force / --no-verify:
//   - Merged: branch tip is reachable from defaultBranch, OR the branch
//     contributes no content defaultBranch does not already have
//     (catches squash- and rebase-merges, whose rewritten commits are
//     never reachable from default)
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

		// Topology is necessary for a merge-commit merge but not for a
		// squash- or rebase-merge: those land rewritten commits on
		// default, so the branch tip is never reachable and the count
		// above stays > 0 even though the work has shipped. Fall back to
		// a content comparison before declaring the branch unmerged.
		// Ordered second because it is the more expensive probe and only
		// the topology answer can be trusted to be cheap.
		if !s.Merged {
			s.Merged = branchContentMergedInto(g, dir, branch, defaultBranch)
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

// branchContentMergedInto reports whether branch contributes any content
// that defaultBranch does not already have. True means the branch is
// content-equivalent to default — the fingerprint a squash- or
// rebase-merge leaves behind, where the shipped commits were rewritten
// and the original tip is unreachable from default.
//
// The probe merges branch into defaultBranch in memory and compares the
// resulting tree with defaultBranch's current tree. Identical trees mean
// merging would be a no-op, i.e. the branch's content already landed.
//
// Two simpler-looking probes were measured and rejected:
//
//   - `git diff <default>...<branch>` (three-dot) is defined as
//     merge-base(default, branch)..branch, so it reports what the branch
//     added since diverging and is blind to default having absorbed it.
//     For a squash-merged branch it is always non-empty — the check would
//     never fire.
//   - `git diff <default> <branch>` (two-dot) compares the two tips
//     wholesale, so any unrelated commit landing on default afterwards
//     makes it non-empty. It answers "are these trees identical", not
//     "does the branch add anything new".
//
// `git cherry` is likewise unsuitable: it matches per-commit patch-ids,
// and a squash-merge collapses N commits into one whose patch-id equals
// none of them.
//
// Fails closed. A false positive here lets `git hop remove --merged`
// delete work that never shipped, so every uncertain path — a missing
// ref, an unreadable tree, a git predating merge-tree --write-tree
// (added in 2.38; this project documents a 2.7 floor) — returns false.
// The cost of a false negative is only that the user passes --force.
func branchContentMergedInto(g git.GitInterface, dir, branch, defaultBranch string) bool {
	// Tree that results from merging branch into defaultBranch. Emits the
	// tree OID on stdout and exits non-zero when a ref cannot be resolved
	// or the subcommand is unsupported.
	merged, err := g.RunInDir(dir, "git", "merge-tree", "--write-tree", defaultBranch, branch)
	if err != nil {
		return false
	}
	mergedTree := strings.TrimSpace(merged)
	if mergedTree == "" {
		return false
	}

	current, err := g.RunInDir(dir, "git", "rev-parse", defaultBranch+"^{tree}")
	if err != nil {
		return false
	}
	currentTree := strings.TrimSpace(current)
	if currentTree == "" {
		return false
	}

	return mergedTree == currentTree
}

// removeGate decides whether the remove can proceed and returns a
// human-readable error explaining what the user must do.
//
// Matrix (per spec):
//
//	merged | pushed | dirty | requires
//	-------|--------|-------|----------------------
//	  no   |   no   |  any  | --force --no-verify
//	  no   |  yes   | dirty | --force --no-verify
//	  no   |  yes   | clean | --force
//	 yes   |  any   | dirty | --no-verify
//	 yes   |  any   | clean | (silent pass)
//
// The two flags answer independent checks. --force covers the
// not-merged check. --no-verify covers uncommitted or untracked files
// (regardless of merge state) and unpushed commits (only relevant when
// unmerged: a merged branch's commits already live on default). Being
// pushed protects the branch's commits, never its worktree files, so a
// dirty worktree always needs --no-verify.
//
// The hint names the complete flag set for the branch's state, not just
// the flags still missing, plus --no-prompt. Satisfying the gate is
// necessary but not sufficient for a scripted removal: any branch that
// trips the gate also trips the confirmation prompt that runs straight
// after, so a hint listing only the gate flags is a dead end on a
// non-interactive stdin. The hint is the full retry that succeeds in
// one shot.
func removeGate(s branchSafety, force, noVerify bool) error {
	dirty := !s.Clean
	needForce := !s.Merged
	needNoVerify := dirty || (!s.Merged && !s.Pushed)

	if (!needForce || force) && (!needNoVerify || noVerify) {
		return nil
	}

	var reasons []string
	switch {
	case !s.Merged && !s.Pushed:
		reasons = append(reasons, "branch is not merged into default and not pushed to origin")
	case !s.Merged:
		reasons = append(reasons, "branch is not merged into default")
	}
	if dirty {
		reasons = append(reasons, "worktree has uncommitted changes or untracked files")
	}

	var flags []string
	if needForce {
		flags = append(flags, "--force")
	}
	if needNoVerify {
		flags = append(flags, "--no-verify")
	}

	return fmt.Errorf(
		"%s; pass %s to remove it anyway "+
			"(add --no-prompt when running non-interactively)",
		strings.Join(reasons, ", and "), strings.Join(flags, " "),
	)
}
