package cmd

import (
	"fmt"

	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
)

// previewLocalConfig is the dry-run view of what a conversion does with
// the repository's local git config. Only key names are printed: values
// can hold credentials (url.<base>.insteadOf, http.extraHeader).
func previewLocalConfig(g git.GitInterface, repoPath, branch string, useBare bool) {
	if !useBare {
		fmt.Println("     - Keep .git: its local config stays in place")
		return
	}
	plan, err := hop.PlanLocalConfig(g, repoPath)
	if err != nil {
		output.Warn("%v", err)
		return
	}
	carried := plan.CarriedKeys()
	fmt.Printf("     - Carry over %d local config key(s):\n", len(carried))
	for _, k := range carried {
		fmt.Printf("         %s\n", k)
	}
	excluded := plan.ExcludedKeys()
	fmt.Printf("     - Leave out %d local config key(s):\n", len(excluded))
	for _, e := range excluded {
		fmt.Printf("         %s: %s\n", e.Key, e.Reason)
	}
	if !plan.HubWorktreeConfig() {
		return
	}
	perWorktree := plan.PerWorktreeKeys()
	fmt.Printf("     - Carry over %d per-worktree config key(s) into hops/%s:\n", len(perWorktree), branch)
	for _, k := range perWorktree {
		fmt.Printf("         %s\n", k)
	}
	if left := plan.PerWorktreeExcludedKeys(); len(left) > 0 {
		fmt.Printf("     - Leave out %d per-worktree config key(s):\n", len(left))
		for _, e := range left {
			fmt.Printf("         %s: %s\n", e.Key, e.Reason)
		}
	}
	fmt.Println("     - Move core.bare into the hub's config.worktree (extensions.worktreeConfig)")
}
