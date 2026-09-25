package cmd

import (
	"github.com/spf13/cobra"

	"hop.top/git/internal/output"
)

// moveResult is the result of `git hop move`: one object. Under --dry-run
// it is the result the move would produce, and carries dry_run.
type moveResult struct {
	OldBranch      string `json:"old_branch" yaml:"old_branch" table:"old_branch" jsonschema:"description=Branch before the move"`
	NewBranch      string `json:"new_branch" yaml:"new_branch" table:"new_branch" jsonschema:"description=Branch after the move"`
	OldPath        string `json:"old_path" yaml:"old_path" table:"old_path" jsonschema:"description=Absolute worktree path before the move"`
	NewPath        string `json:"new_path" yaml:"new_path" table:"new_path" jsonschema:"description=Absolute worktree path after the move"`
	CurrentUpdated bool   `json:"current_updated" yaml:"current_updated" table:"current_updated" jsonschema:"description=True when the hub's current symlink pointed at the moved worktree and now points at its new path (or with --dry-run would)"`
	DryRun         bool   `json:"dry_run,omitempty" yaml:"dry_run,omitempty" jsonschema:"description=True under --dry-run: the result is what the move would produce; absent otherwise"`
}

func init() {
	declareOutputSchema(moveCmd, &moveResult{})
}

// emitMoveResult renders res as move's result in the structured modes;
// the human view has already reported the move.
func emitMoveResult(cmd *cobra.Command, res moveResult) {
	if output.IsStructured() {
		emitResult(cmd, res)
	}
}
