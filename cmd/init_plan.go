package cmd

import (
	"fmt"

	"hop.top/git/internal/git"
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
// working tree and adds worktrees/ beside it.
func initConversionPlan(backupRoot, branch string, useBare bool) []string {
	if branch == "" {
		branch = "(detached HEAD)"
	}
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
