package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"hop.top/git/internal/hooks"
	"hop.top/git/internal/output"
)

// exitUsage is git's status for a usage error.
const exitUsage = 129

// dryRunAnnotation marks a command that honors the global --dry-run: it
// either previews its writes and stops before the first one, or it
// performs none.
const dryRunAnnotation = "hop.top/git/dry-run"

// SupportDryRun declares that each of cmds honors the global --dry-run.
//
// Support is opt-in on purpose. --dry-run is a persistent root flag, so
// every command parses it; a command that never reads it would apply its
// changes while the user believes nothing happens. checkDryRunSupported
// refuses the flag on any command not marked here, so a new mutating
// command fails closed instead of silently mutating.
func SupportDryRun(cmds ...*cobra.Command) {
	for _, c := range cmds {
		if c.Annotations == nil {
			c.Annotations = map[string]string{}
		}
		c.Annotations[dryRunAnnotation] = "true"
	}
}

// RejectDryRun ends the process with a usage error for an operation that
// cannot preview itself. Call it before the first write.
func RejectDryRun(operation string) {
	output.FatalCode(exitUsage, "--dry-run is not supported for %s; run it without --dry-run", operation)
}

// checkDryRunSupported returns an error when the global --dry-run is set on
// a command that has not declared support for it.
//
// A command that registers its own local --dry-run shadows the global one
// (cobra merges persistent flags only under names not already taken), so
// the flag it parsed is not the root's and it is left to handle the flag
// itself. Cobra's help and completion plumbing run no command logic.
func checkDryRunSupported(cmd *cobra.Command) error {
	global := cmd.Root().PersistentFlags().Lookup("dry-run")
	if global == nil || cmd.Flags().Lookup("dry-run") != global || global.Value.String() != "true" {
		return nil
	}
	if _, ok := cmd.Annotations[dryRunAnnotation]; ok {
		return nil
	}
	switch cmd.Name() {
	case "help", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return nil
	}
	name := strings.TrimSpace(strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()))
	return fmt.Errorf("--dry-run is not supported by 'git hop %s'", name)
}

// previewSwitch reports what switching to branch would do without doing
// any of it: no hook runs and the `current` symlink is not written.
func previewSwitch(fs afero.Fs, uri, repoID, branch, worktreePath string) {
	runner := hooks.NewRunner(fs).ForRepo(uri)
	output.Info("[dry-run] Would switch to worktree '%s'", branch)
	PreviewHook(runner, "pre-worktree-switch", worktreePath, repoID)
	output.Info("[dry-run] Would point 'current' at %s", worktreePath)
	PreviewHook(runner, "post-worktree-switch", worktreePath, repoID)
}

// PreviewHook reports the lifecycle hook a real run would dispatch, if one
// resolves, without running it.
func PreviewHook(runner *hooks.Runner, name, worktreePath, repoID string) {
	if f := runner.FindHookFile(name, worktreePath, repoID); f != "" {
		output.Info("[dry-run] Would run hook %s (%s)", name, f)
	}
}
