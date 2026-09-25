package cmd

import (
	"sort"

	"github.com/spf13/cobra"

	"hop.top/git/internal/output"
)

// removeRecord is one entry of the `git hop remove` result: a worktree
// removed or left in place, or, when a whole hub is removed, the hub and
// the repository's data-home hopspace. Every form of remove renders a list
// of them: one for a branch, one per candidate for --merged, and for a hub
// its worktrees, the hub, then the hopspace when there is one.
//
// Under --dry-run the records are the ones the run would produce, and
// carry dry_run.
type removeRecord struct {
	Kind          string `json:"kind" yaml:"kind" table:"kind" jsonschema:"enum=worktree,enum=hub,enum=hopspace,description=worktree: a branch worktree; hub: the hub directory (remove <hub>); hopspace: the repository's data-home hopspace (remove <hub>, when it exists)"`
	Branch        string `json:"branch" yaml:"branch" table:"branch" jsonschema:"description=Branch of a worktree record; empty for hub and hopspace"`
	Path          string `json:"path" yaml:"path" table:"path" jsonschema:"description=Absolute path of the worktree / hub / hopspace"`
	Removed       bool   `json:"removed" yaml:"removed" table:"removed" jsonschema:"description=True when the entry was removed (or with --dry-run would be); false when it was left in place (see reason)"`
	BranchDeleted bool   `json:"branch_deleted" yaml:"branch_deleted" table:"branch_deleted" jsonschema:"description=True when the local branch was deleted (or would be); false when it was already absent or the entry is not a branch worktree"`
	RemoteDeleted bool   `json:"remote_deleted" yaml:"remote_deleted" table:"remote_deleted" jsonschema:"description=True when the branch was deleted on origin (--delete-remote); with --dry-run true when --delete-remote would try (origin is not probed)"`
	Reason        string `json:"reason,omitempty" yaml:"reason,omitempty" jsonschema:"description=Why the entry was left in place: the safety gate's refusal / a failure / a --merged skip / the hopspace still in use; absent when removed"`
	DryRun        bool   `json:"dry_run,omitempty" yaml:"dry_run,omitempty" jsonschema:"description=True under --dry-run: the record is what the run would produce; absent otherwise"`
	// Volumes and VolumePaths say what a hub or hopspace removal did
	// with the volume data in it.
	Volumes     string   `json:"volumes,omitempty" yaml:"volumes,omitempty" jsonschema:"enum=kept,enum=deleted,description=Hub and hopspace records: what the removal did (with --dry-run: would do) with the volume data in it. kept is the default: moved aside to $GIT_HOP_DATA_HOME/orphaned-volumes/<org>/<repo>/<name>-<UTC> or left in place when it cannot be moved or the hopspace stays; deleted is --delete-volumes; absent when it holds none"`
	VolumePaths []string `json:"volume_paths,omitempty" yaml:"volume_paths,omitempty" jsonschema:"description=Where the kept volume data is now or where the deleted data was; with --dry-run where it is now"`
}

const (
	removeKindWorktree = "worktree"
	removeKindHub      = "hub"
	removeKindHopspace = "hopspace"
)

func init() {
	declareOutputSchema(removeCmd, &[]removeRecord{})
}

// sortRemoveRecords orders records by branch, the order --merged
// reports its candidates in whatever order the hub map yields them.
func sortRemoveRecords(recs []removeRecord) {
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].Branch < recs[j].Branch })
}

// markDryRun flags every record as a preview.
func markDryRun(recs []removeRecord) []removeRecord {
	for i := range recs {
		recs[i].DryRun = true
	}
	return recs
}

// emitRemoveResult renders recs as remove's result in the structured
// modes; the human view has already reported the run. An empty result is
// an empty list, never empty stdout.
func emitRemoveResult(cmd *cobra.Command, recs []removeRecord) {
	if !output.IsStructured() {
		return
	}
	if recs == nil {
		recs = []removeRecord{}
	}
	emitResult(cmd, recs)
}
