package cmd

import (
	"fmt"

	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

// initRemoteLabel is the Remote: value init shows: "origin (<url>)", or
// "none" when the repository has no origin.
func initRemoteLabel(g git.GitInterface, repoPath string) string {
	if url, err := g.GetRemoteURL(repoPath); err == nil && url != "" {
		return fmt.Sprintf("origin (%s)", url)
	}
	return "none"
}

// initConversionPlan is the step list `init --dry-run` prints. It
// describes what ConvertToBareWorktree does for the chosen layout: a bare
// conversion checks the current branch out at hops/<branch> and points
// current at it; a regular one keeps the repository root as that branch's
// working tree and adds worktrees/ beside it. branch is never empty: a
// detached HEAD is refused before the plan (see refuseDetachedHead).
// linked is the bare conversion's carry of the linked worktrees.
func initConversionPlan(backupRoot, branch string, useBare bool, linked *hop.LinkedCarryPlan) []string {
	plan := []string{
		fmt.Sprintf("  1. Create backup in %s/", backupRoot),
		"  2. Create worktree structure",
	}
	if useBare {
		plan = append(plan,
			"     - Convert to bare repository",
			fmt.Sprintf("     - Create hops/%s/ worktree for the %s branch", branch, branch),
			fmt.Sprintf("     - Point current at hops/%s", branch),
		)
		plan = append(plan, initLinkedCarrySteps(linked)...)
	} else {
		plan = append(plan,
			fmt.Sprintf("     - Keep the repository root as the %s branch working tree", branch),
			"     - Create worktrees/ for future branch worktrees",
		)
	}
	return append(plan,
		"  3. Create hop.json configuration",
		"  4. Register in global registry",
	)
}

// initSetUpStep is the plan's last step, printed after the local config
// preview: the initial worktree's set-up (setUpInitWorktree), which runs
// with or without --no-hooks.
func initSetUpStep(branch string) string {
	return fmt.Sprintf("  5. Set up the %s worktree's environment, when it has one: ports,\n"+
		"     volumes, .env, compose override, then shared dependencies", branch)
}

// initLinkedCarrySteps lists, for the plan, what a bare conversion does
// with each linked worktree: carried where it is, or moved into hops/
// when it sits inside the working tree; a prunable one is left behind.
func initLinkedCarrySteps(linked *hop.LinkedCarryPlan) []string {
	if linked == nil {
		return nil
	}
	var steps []string
	for _, w := range linked.Worktrees {
		what := "detached HEAD"
		if w.Branch != "" {
			what = w.Branch
		}
		if w.Dest != "" {
			steps = append(steps, fmt.Sprintf("     - Carry linked worktree %s (%s), moved to %s", w.Path, what, w.Dest))
			continue
		}
		steps = append(steps, fmt.Sprintf("     - Carry linked worktree %s (%s), where it is", w.Path, what))
	}
	for _, p := range linked.Prunable {
		steps = append(steps, fmt.Sprintf("     - Leave prunable linked worktree %s behind", p))
	}
	return steps
}
