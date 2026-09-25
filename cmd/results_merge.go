package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"hop.top/git/internal/git"
	"hop.top/git/internal/output"
)

// mergeResult is the result of `git hop merge`: one object. Under
// --dry-run it is the result the merge would produce, and carries dry_run.
type mergeResult struct {
	Source        string `json:"source" yaml:"source" table:"source" jsonschema:"description=Branch merged"`
	Into          string `json:"into" yaml:"into" table:"into" jsonschema:"description=Receiving branch"`
	Result        string `json:"result" yaml:"result" table:"result" jsonschema:"enum=up-to-date,enum=fast-forward,enum=merge-commit,description=up-to-date: into already contained source; fast-forward: into moved to source's tip; merge-commit: a merge commit was created"`
	Commit        string `json:"commit" yaml:"commit" table:"commit" jsonschema:"description=Full id of the commit the receiving branch ends on; with --dry-run empty for a merge-commit (it does not exist yet)"`
	SourceRemoved bool   `json:"source_removed" yaml:"source_removed" table:"source_removed" jsonschema:"description=True when the source worktree directory was removed (or with --dry-run would be)"`
	BranchDeleted bool   `json:"branch_deleted" yaml:"branch_deleted" table:"branch_deleted" jsonschema:"description=True when the local source branch was deleted (or with --dry-run would be)"`
	RemoteDeleted bool   `json:"remote_deleted" yaml:"remote_deleted" table:"remote_deleted" jsonschema:"description=True when the source branch was deleted on origin (--delete-remote / hop.merge.deleteRemote); with --dry-run true when it would try (origin is not probed)"`
	DryRun        bool   `json:"dry_run,omitempty" yaml:"dry_run,omitempty" jsonschema:"description=True under --dry-run: the result is what the merge would produce; absent otherwise"`
}

// The values of mergeResult.Result.
const (
	mergeUpToDate    = "up-to-date"
	mergeFastForward = "fast-forward"
	mergeCommit      = "merge-commit"
)

func init() {
	declareOutputSchema(mergeCmd, &mergeResult{})
}

// emitMergeResult renders res as merge's result in the structured modes;
// the human view has already reported the merge.
func emitMergeResult(cmd *cobra.Command, res mergeResult) {
	if output.IsStructured() {
		emitResult(cmd, res)
	}
}

// revParse returns the full id rev names in the repository at dir, or ""
// when it names nothing.
func revParse(g git.GitInterface, dir, rev string) string {
	out, err := g.RunInDir(dir, "git", "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// mergeOutcome names what `git merge` did to the branch checked out at
// dir, given its HEAD before the merge: nothing, a fast-forward, or a new
// merge commit (a HEAD with a second parent).
func mergeOutcome(g git.GitInterface, dir, before, after string) string {
	switch {
	case after == before:
		return mergeUpToDate
	case revParse(g, dir, after+"^2") != "":
		return mergeCommit
	default:
		return mergeFastForward
	}
}
