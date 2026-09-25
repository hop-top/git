package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"

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
// working tree and adds worktrees/ beside it. branch is never empty: a
// detached HEAD is refused before the plan (see refuseDetachedHead).
func initConversionPlan(backupRoot, branch string, useBare bool) []string {
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

// initProceedFlags are the init flags that shape a conversion, in the
// order the dry run's closing hint repeats them.
var initProceedFlags = []string{
	"no-prompt", "regular", "force", "keep-backup",
	"no-hooks", "hooks", "hooks-overwrite", "enable-chdir",
}

// initProceedCommand is the command the dry run's closing hint offers:
// init with every conversion flag the user gave, as given, so it runs
// the conversion just previewed. -n is dropped; unset flags stay unset.
func initProceedCommand(flags *pflag.FlagSet) string {
	parts := []string{"git hop init"}
	if flags == nil {
		return parts[0]
	}
	for _, name := range initProceedFlags {
		f := flags.Lookup(name)
		if f == nil || !f.Changed {
			continue
		}
		if f.Value.Type() == "bool" && f.Value.String() == "true" {
			parts = append(parts, "--"+name)
			continue
		}
		parts = append(parts, fmt.Sprintf("--%s=%s", name, f.Value.String()))
	}
	return strings.Join(parts, " ")
}

// initSetUpStep is the plan's last step, printed after the local config
// preview: the initial worktree's set-up (setUpInitWorktree), which runs
// with or without --no-hooks.
func initSetUpStep(branch string) string {
	return fmt.Sprintf("  5. Set up the %s worktree's environment, when it has one: ports,\n"+
		"     volumes, .env, compose override, then shared dependencies", branch)
}
