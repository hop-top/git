package cmd

import (
	"path/filepath"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

// What `git hop init` did, the action of its result.
const (
	initActionConverted          = "converted"
	initActionAdopted            = "adopted"
	initActionAlreadyInitialized = "already-initialized"
	initActionRestored           = "restored"
)

// initResult is the result of `git hop init`: one object whatever the
// mode, told apart by action. A dry run, in every mode, is the result
// the real run would have, with dry_run set. Register-as-is (menu option 3) is only
// reachable through the interactive menu, which a structured run cannot
// show, so it has no action.
type initResult struct {
	Action        string         `json:"action" yaml:"action" table:"action" jsonschema:"enum=converted,enum=adopted,enum=already-initialized,enum=restored,description=converted: a standard repository was converted to the worktree layout; adopted: a bare or regular hub git-hop did not create got its missing hop.json and was registered; already-initialized: the hub was set up already and nothing was created; restored: --restore put a conversion backup back. Under --dry-run: what init would do"`
	Hub           string         `json:"hub" yaml:"hub" table:"hub" jsonschema:"description=Absolute path of the hub; for restored the location the backup was restored to"`
	Layout        string         `json:"layout" yaml:"layout" table:"layout" jsonschema:"description=bare (a bare repository with worktrees under hops/) or regular (the repository root is the default branch's worktree); empty for restored"`
	DefaultBranch string         `json:"default_branch" yaml:"default_branch" table:"default_branch" jsonschema:"description=Default branch of the hub; empty for restored or when the hub records none"`
	Backup        string         `json:"backup" yaml:"backup" table:"backup" jsonschema:"description=Conversion backup: the one taken (converted; empty under --dry-run) or the one restored from (restored); empty otherwise"`
	DryRun        bool           `json:"dry_run,omitempty" yaml:"dry_run,omitempty" jsonschema:"description=True when --dry-run previewed the run and changed nothing; absent otherwise"`
	BackupKept    bool           `json:"backup_kept,omitempty" yaml:"backup_kept,omitempty" jsonschema:"description=True when the backup is still on disk after the conversion (--keep-backup or hop.backup.keepBackup; under --dry-run: would be); absent otherwise"`
	Registered    bool           `json:"registered,omitempty" yaml:"registered,omitempty" jsonschema:"description=True when this run recorded the hub in git-hop's state (converted and adopted; under --dry-run: would); absent otherwise"`
	Worktrees     []initWorktree `json:"worktrees" yaml:"worktrees" jsonschema:"description=Worktrees this run created and then the linked worktrees a bare conversion carried into the hub (under --dry-run: would create and carry); empty when there are none"`
	MovedAside    string         `json:"moved_aside,omitempty" yaml:"moved_aside,omitempty" jsonschema:"description=restored with --force only: where what occupied the original location was moved (under --dry-run: would be); absent otherwise"`
}

// What `git hop init` did with a worktree of its result.
const (
	initWorktreeCreated = "created"
	initWorktreeCarried = "carried"
)

// initWorktree is one worktree `git hop init` created, or one linked
// worktree a bare conversion carried into the hub.
type initWorktree struct {
	Branch    string `json:"branch" yaml:"branch" jsonschema:"description=Branch checked out in the worktree; empty for a carried worktree on a detached HEAD"`
	Path      string `json:"path" yaml:"path" jsonschema:"description=Absolute path of the worktree (for carried: where it is after the conversion)"`
	Action    string `json:"action" yaml:"action" jsonschema:"enum=created,enum=carried,description=created: init checked the worktree out; carried: a linked worktree of the converted repository adopted by the hub (one outside the working tree stays where it is and one inside moves to hops/<branch>). Under --dry-run: what init would do"`
	MovedFrom string `json:"moved_from,omitempty" yaml:"moved_from,omitempty" jsonschema:"description=carried only: the worktree's path before the conversion when it moved into hops/; absent otherwise"`
}

// initCarriedWorktrees lists the linked worktrees a bare conversion
// carried (or, from the plan, would carry) as result worktrees.
func initCarriedWorktrees(carried []config.CarriedWorktree) []initWorktree {
	out := make([]initWorktree, 0, len(carried))
	for _, w := range carried {
		out = append(out, initWorktree{Branch: w.Branch, Path: w.Path, Action: initWorktreeCarried, MovedFrom: w.MovedFrom})
	}
	return out
}

// initPlannedCarry is what the carry plan would leave in the hub at
// repoPath, in the plan's order; nil without a plan.
func initPlannedCarry(plan *hop.LinkedCarryPlan, repoPath string) []config.CarriedWorktree {
	if plan == nil {
		return nil
	}
	carried := make([]config.CarriedWorktree, 0, len(plan.Worktrees))
	for _, w := range plan.Worktrees {
		carried = append(carried, w.Carried(repoPath))
	}
	return carried
}

// initLayout names the layout of a conversion.
func initLayout(useBare bool) string {
	if useBare {
		return "bare"
	}
	return "regular"
}

// initConversionResult is the result of a conversion of repoPath that
// leaves branch checked out at worktreePath and carries the linked
// worktrees in carried.
func initConversionResult(repoPath, branch, worktreePath string, useBare bool, carried []config.CarriedWorktree) initResult {
	res := initResult{
		Action:        initActionConverted,
		Hub:           repoPath,
		Layout:        initLayout(useBare),
		DefaultBranch: branch,
		Worktrees:     []initWorktree{},
	}
	if worktreePath != "" {
		res.Worktrees = append(res.Worktrees, initWorktree{Branch: branch, Path: worktreePath, Action: initWorktreeCreated})
	}
	res.Worktrees = append(res.Worktrees, initCarriedWorktrees(carried)...)
	return res
}

// initHubResult is the result for a hub init found already set up
// (action already-initialized or adopted): its layout and default
// branch as they are on disk, and no worktree created.
func initHubResult(fs afero.Fs, g git.GitInterface, hubPath, action string) initResult {
	res := initResult{Action: action, Hub: hubPath, Worktrees: []initWorktree{}}
	switch hop.DetectRepoStructure(fs, g, hubPath) {
	case config.BareWorktreeRoot:
		res.Layout = "bare"
	case config.WorktreeRoot:
		res.Layout = "regular"
	}
	if hub, err := hop.LoadHub(fs, hubPath); err == nil {
		res.DefaultBranch = hub.Config.Repo.DefaultBranch
	}
	return res
}

// previewAlreadyInitializedResult is the result `init --dry-run` reports
// for a hub already set up: adopted when init would back-fill its
// missing hop.json and register it, already-initialized otherwise.
// Nothing is written.
func previewAlreadyInitializedResult(fs afero.Fs, g git.GitInterface, path string, structure config.StructureType) initResult {
	hubPath, adopt := path, false
	if root, ok := resolveBackfillRoot(fs, g, path, structure); ok {
		hubPath = root
		exists, _ := afero.Exists(fs, filepath.Join(hubPath, "hop.json"))
		adopt = !exists
	}
	if !adopt {
		res := initHubResult(fs, g, hubPath, initActionAlreadyInitialized)
		res.DryRun = true
		return res
	}
	res := initHubResult(fs, g, hubPath, initActionAdopted)
	res.DefaultBranch = backfillDefaultBranch(g, hubPath)
	res.Registered = true
	res.DryRun = true
	return res
}
