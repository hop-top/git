package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
)

// Structured results for the commands that support --format json|yaml|csv|
// text, --json and --porcelain. Each type is declared as the command's
// output schema (see declareOutputSchema), so the schema published to
// agents is reflected from the same struct the command renders and cannot
// drift from it.
//
// Tag roles: json/yaml name the keys of the structured documents; table
// names the columns of the columnar formats (csv, text, --porcelain) and
// fixes their order, which is the declaration order below. A field
// without a table tag appears in json/yaml only.

// resultSchemaVersion is the MAJOR.MINOR of the result shapes below.
// Bump MINOR for additive fields, MAJOR for renames and removals.
const resultSchemaVersion = "1.0"

// addResult is the result of `git hop add`.
type addResult struct {
	Branch   string         `json:"branch" yaml:"branch" table:"branch" jsonschema:"description=Branch checked out in the new worktree"`
	Path     string         `json:"path" yaml:"path" table:"path" jsonschema:"description=Absolute path of the new worktree"`
	Base     string         `json:"base" yaml:"base" table:"base" jsonschema:"description=Branch that status and list compare this worktree against"`
	Upstream string         `json:"upstream" yaml:"upstream" table:"upstream" jsonschema:"description=Upstream the branch tracks (for example origin/main); empty when it tracks none"`
	Created  bool           `json:"created" yaml:"created" table:"created" jsonschema:"description=True when this run created the local branch; false when an existing local branch was checked out"`
	Ports    map[string]int `json:"ports,omitempty" yaml:"ports,omitempty" jsonschema:"description=Port allocated to each service when the worktree has a Docker environment"`
}

// statusRecord is one worktree row of `git hop status` inside a hub.
type statusRecord struct {
	Branch string `json:"branch" yaml:"branch" table:"branch" jsonschema:"description=Branch name"`
	Base   string `json:"base" yaml:"base" table:"base" jsonschema:"description=Branch the sync status is computed against"`
	State  string `json:"state" yaml:"state" table:"state" jsonschema:"description=Linked when the worktree directory exists; Missing otherwise"`
	Status string `json:"status" yaml:"status" table:"status" jsonschema:"description=Sync label relative to base (default/synced/N ahead/behind (N)/merged/diverged; optional dirty suffix); - when missing"`
	Path   string `json:"path" yaml:"path" table:"path" jsonschema:"description=Absolute path of the worktree"`
}

// listRecord is one worktree row of `git hop list`. The repository column
// is always present so the record has one shape whether list runs inside a
// hub (one repository) or outside one (every tracked repository).
type listRecord struct {
	Repository string `json:"repository" yaml:"repository" table:"repository" jsonschema:"description=Repository id (host/org/repo)"`
	Branch     string `json:"branch" yaml:"branch" table:"branch" jsonschema:"description=Branch name"`
	Base       string `json:"base" yaml:"base" table:"base" jsonschema:"description=Branch the sync status is computed against"`
	Type       string `json:"type" yaml:"type" table:"type" jsonschema:"description=Worktree type recorded in state (bare or linked)"`
	Path       string `json:"path" yaml:"path" table:"path" jsonschema:"description=Absolute path of the worktree"`
	State      string `json:"state" yaml:"state" table:"state" jsonschema:"description=active when the worktree directory exists; missing otherwise"`
	Status     string `json:"status" yaml:"status" table:"status" jsonschema:"description=Sync label relative to base (default/synced/N ahead/behind (N)/merged/diverged; optional dirty suffix); - when missing"`
}

// declareOutputSchema publishes shape as cmd's output schema. The
// declaration is also what opts cmd into structured output: the root only
// honours --format/--json/--porcelain for commands that declare one.
func declareOutputSchema(cmd *cobra.Command, shape any) {
	if err := kitcli.SetOutputSchema(cmd, kitcli.OutputSchema{
		Type:    shape,
		Version: resultSchemaVersion,
	}); err != nil {
		panic(err)
	}
}

// emitResult renders data as the command's structured result. A render
// failure is an operation failure: the caller asked for a result it did
// not get.
func emitResult(cmd *cobra.Command, data any) {
	if err := output.EmitResult(cmd, data); err != nil {
		output.Fatal("%v", err)
	}
}

// localBranchExists reports whether refs/heads/<branch> exists in the
// repository at dir.
func localBranchExists(g git.GitInterface, dir, branch string) bool {
	_, err := g.RevParse(dir, "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// branchUpstream returns the upstream of the branch checked out at dir in
// its short form (origin/main), or "" when it tracks none.
func branchUpstream(g git.GitInterface, dir string) string {
	out, err := g.RunInDir(dir, "git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// newAddResult assembles the add result once the worktree is registered
// in the hub, so base reflects what status and list will report for it.
func newAddResult(g git.GitInterface, hub *hop.Hub, branch, worktreePath string, created bool, ports *config.BranchPorts) addResult {
	res := addResult{
		Branch:   branch,
		Path:     worktreePath,
		Base:     hub.Config.Repo.DefaultBranch,
		Upstream: branchUpstream(g, worktreePath),
		Created:  created,
	}
	if b, ok := hub.Config.Branches[branch]; ok {
		res.Base = resolveCompareBranch(hub.Config, b)
	}
	if ports != nil && len(ports.Ports) > 0 {
		res.Ports = ports.Ports
	}
	return res
}
