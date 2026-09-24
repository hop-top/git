package cmd

import (
	"fmt"
)

// unregisteredBareWorktreeHint composes the message printed when status
// detects a bare-worktree-shaped repo missing hop.json. The shape is
// modeled on git's own "you need to do X" notices: a short factual line
// followed by a lowercase "hint:" line carrying the recovery suggestion.
// Detection itself is hop.FindUnregisteredHub.
func unregisteredBareWorktreeHint(repoRoot string) string {
	return fmt.Sprintf(
		"Detected a bare-worktree repository at %s, but it is missing hop.json.\n"+
			"hint: run 'git hop init' at %s to register it as a hub.",
		repoRoot, repoRoot,
	)
}
