package cli

import (
	"github.com/spf13/cobra"

	"hop.top/git/internal/output"
)

// What the root command did, the action of its result.
const (
	rootActionSwitched = "switched"
)

// RootResult is the result of the root command, `git hop <arg>`: one
// object, told apart by action. The columns every action fills (action,
// branch, path, hub) are the columnar layout; what only one action knows
// is json/yaml only. Under --dry-run it is the result the run would
// produce, with dry_run set.
//
// The root declares one shape because a cobra command carries one output
// schema, and switch, clone and fork-attach all run as the root.
type RootResult struct {
	Action         string `json:"action" yaml:"action" table:"action" jsonschema:"enum=switched,description=switched: git hop <branch> made an existing worktree current. Under --dry-run: what the run would do"`
	Branch         string `json:"branch" yaml:"branch" table:"branch" jsonschema:"description=Branch of the worktree the run landed on"`
	Path           string `json:"path" yaml:"path" table:"path" jsonschema:"description=Absolute path of that worktree"`
	Hub            string `json:"hub" yaml:"hub" table:"hub" jsonschema:"description=Absolute path of the hub the worktree belongs to"`
	CurrentUpdated *bool  `json:"current_updated,omitempty" yaml:"current_updated,omitempty" jsonschema:"description=switched only: true when the hub's current symlink now points at the worktree (or with --dry-run would); false when writing it failed"`
	DryRun         bool   `json:"dry_run,omitempty" yaml:"dry_run,omitempty" jsonschema:"description=True under --dry-run: the result is what the run would produce; absent otherwise"`
}

// emitRootResult renders res as the root command's result in the
// structured modes; the human view has already reported the run. A render
// failure is an operation failure: the caller asked for a result it did
// not get.
func emitRootResult(cmd *cobra.Command, res RootResult) {
	if !output.IsStructured() {
		return
	}
	if err := output.EmitResult(cmd, res); err != nil {
		output.Fatal("%v", err)
	}
}

// switchResult is the result of switching to branch's worktree at path in
// the hub at hubPath.
func switchResult(branch, path, hubPath string, currentUpdated bool) RootResult {
	return RootResult{
		Action:         rootActionSwitched,
		Branch:         branch,
		Path:           path,
		Hub:            hubPath,
		CurrentUpdated: &currentUpdated,
	}
}
