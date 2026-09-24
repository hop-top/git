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
func previewLocalConfig(g git.GitInterface, repoPath string, useBare bool) {
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
}
