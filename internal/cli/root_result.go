package cli

import (
	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
)

// What the root command did, the action of its result.
const (
	rootActionSwitched     = "switched"
	rootActionCloned       = "cloned"
	rootActionForkAttached = "fork-attached"
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
	Action         string `json:"action" yaml:"action" table:"action" jsonschema:"enum=switched,enum=cloned,enum=fork-attached,description=switched: git hop <branch> made an existing worktree current; cloned: git hop <uri> created a hub and its default branch's worktree; fork-attached: git hop <uri> --branch <b> attached a fork's branch to the current hub. Under --dry-run: what the run would do"`
	Branch         string `json:"branch" yaml:"branch" table:"branch" jsonschema:"description=Branch of the worktree the run landed on: the one switched to; the default branch (cloned); the hub branch the fork's branch was attached as, <branch>-fork-<owner> (fork-attached)"`
	Path           string `json:"path" yaml:"path" table:"path" jsonschema:"description=Absolute path of that worktree"`
	Hub            string `json:"hub" yaml:"hub" table:"hub" jsonschema:"description=Absolute path of the hub the worktree belongs to"`
	URI            string `json:"uri,omitempty" yaml:"uri,omitempty" jsonschema:"description=cloned: the origin cloned; fork-attached: the fork's URI; absent for switched"`
	DefaultBranch  string `json:"default_branch,omitempty" yaml:"default_branch,omitempty" jsonschema:"description=cloned only: the origin's default branch; absent otherwise"`
	ForkBranch     string `json:"fork_branch,omitempty" yaml:"fork_branch,omitempty" jsonschema:"description=fork-attached only: the fork's branch that was attached; absent otherwise"`
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

// cloneResult is the result of cloning uri into the hub at hubPath, read
// back from the hub the clone wrote: its default branch and that
// branch's worktree.
func cloneResult(fs afero.Fs, uri, hubPath string) (RootResult, error) {
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		return RootResult{}, err
	}
	def := hub.Config.Repo.DefaultBranch
	return RootResult{
		Action:        rootActionCloned,
		Branch:        def,
		Path:          hub.WorktreePaths()[def],
		Hub:           hubPath,
		URI:           uri,
		DefaultBranch: def,
	}, nil
}

// forkAttachResult is the result of attaching uri's forkBranch to the hub
// at hubPath as a.
func forkAttachResult(a hop.ForkAttachment, uri, forkBranch, hubPath string) RootResult {
	return RootResult{
		Action:     rootActionForkAttached,
		Branch:     a.Branch,
		Path:       a.Path,
		Hub:        hubPath,
		URI:        uri,
		ForkBranch: forkBranch,
	}
}
