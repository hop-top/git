package cmd

import (
	"strings"

	"github.com/invopop/jsonschema"
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
const resultSchemaVersion = "1.5"

// addResult is the result of `git hop add`.
type addResult struct {
	Branch     string         `json:"branch" yaml:"branch" table:"branch" jsonschema:"description=Branch checked out in the new worktree"`
	Path       string         `json:"path" yaml:"path" table:"path" jsonschema:"description=Absolute path of the new worktree"`
	Base       string         `json:"base" yaml:"base" table:"base" jsonschema:"description=Branch that status and list compare this worktree against"`
	Upstream   string         `json:"upstream" yaml:"upstream" table:"upstream" jsonschema:"description=Upstream the branch tracks (for example origin/main); empty when it tracks none"`
	Created    bool           `json:"created" yaml:"created" table:"created" jsonschema:"description=True when this run created the local branch; false when an existing local branch was checked out"`
	Ports      map[string]int `json:"ports,omitempty" yaml:"ports,omitempty" jsonschema:"description=Port allocated to each service when the worktree has a Docker environment"`
	Task       string         `json:"task,omitempty" yaml:"task,omitempty" jsonschema:"description=Task id recorded for the worktree with --task; absent when none"`
	EnvStarted bool           `json:"env_started,omitempty" yaml:"env_started,omitempty" jsonschema:"description=True when add started the worktree's environment (--env-start or hop.env.autoStart); absent otherwise"`
}

// statusRecord is one worktree row of `git hop status`. Every status view
// renders a list of them: the hub's worktrees (default), the one named
// branch (status <branch>), or every tracked repository's worktrees
// (status --all). The fields without a table tag are view-specific and
// json/yaml only, so the columnar formats keep the same columns in every
// view.
type statusRecord struct {
	Branch     string          `json:"branch" yaml:"branch" table:"branch" jsonschema:"description=Branch name"`
	Base       string          `json:"base" yaml:"base" table:"base" jsonschema:"description=Branch the sync status is computed against"`
	State      string          `json:"state" yaml:"state" table:"state" jsonschema:"description=Linked when the worktree directory exists; Missing otherwise"`
	Status     string          `json:"status" yaml:"status" table:"status" jsonschema:"description=Sync label relative to base (default/synced/N ahead/behind (N)/merged/diverged; optional dirty suffix); - when missing"`
	Path       string          `json:"path" yaml:"path" table:"path" jsonschema:"description=Absolute path of the worktree"`
	Repository string          `json:"repository,omitempty" yaml:"repository,omitempty" jsonschema:"description=Repository id (host/org/repo); status --all only"`
	Ports      map[string]int  `json:"ports,omitempty" yaml:"ports,omitempty" jsonschema:"description=Port allocated to each service; status <branch> only when ports are allocated"`
	Services   []statusService `json:"services,omitempty" yaml:"services,omitempty" jsonschema:"description=Docker Compose containers of the worktree; status <branch> only when it defines services"`
}

// statusService is one Docker Compose container of a worktree, as
// reported by docker compose ps.
type statusService struct {
	Service string `json:"service" yaml:"service" jsonschema:"description=Compose service name"`
	Name    string `json:"name" yaml:"name" jsonschema:"description=Container name"`
	State   string `json:"state" yaml:"state" jsonschema:"description=Container state (running/exited/...)"`
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

// doctorRecord is one line of the `git hop doctor` report: a problem a
// check found, or what --fix did (or, with --dry-run, would do) about one.
// A healthy run reports no records.
type doctorRecord struct {
	Kind    string `json:"kind" yaml:"kind" table:"kind" jsonschema:"enum=issue,enum=warning,enum=fixed,enum=would-fix,enum=failed,description=issue: a problem that makes the installation unhealthy; warning: reported but harmless; fixed: repaired by --fix; would-fix: --fix --dry-run would repair it; failed: --fix could not repair it"`
	Check   string `json:"check" yaml:"check" table:"check" jsonschema:"description=Check that produced the record"`
	Subject string `json:"subject" yaml:"subject" table:"subject" jsonschema:"description=What the record is about: a path / branch / repository:branch / dependency key"`
	Message string `json:"message" yaml:"message" table:"message" jsonschema:"description=Human-readable description"`
}

// JSONSchemaExtend sets the schema's check enum from doctorChecks, so
// the schema lists every check doctor emits and a new one cannot be left
// out.
func (doctorRecord) JSONSchemaExtend(s *jsonschema.Schema) {
	check, ok := s.Properties.Get("check")
	if !ok {
		return
	}
	check.Enum = make([]any, len(doctorChecks))
	for i, name := range doctorChecks {
		check.Enum[i] = name
	}
}

// pruneRecord is one entry `git hop prune` removed or, with --dry-run,
// would remove.
type pruneRecord struct {
	Action     string `json:"action" yaml:"action" table:"action" jsonschema:"enum=pruned,enum=would-prune,enum=skipped,description=pruned; would-prune under --dry-run; skipped: left in place although its path is missing (see reason)"`
	Kind       string `json:"kind" yaml:"kind" table:"kind" jsonschema:"enum=worktree,enum=hub,enum=hop-json-entry,enum=repair-backup,description=worktree and hub: entries in the state file; hop-json-entry: a hub's hop.json branch entry; repair-backup: an expired repair backup directory"`
	Repository string `json:"repository" yaml:"repository" table:"repository" jsonschema:"description=Repository id (host/org/repo) the entry belongs to"`
	Branch     string `json:"branch" yaml:"branch" table:"branch" jsonschema:"description=Branch of a worktree or hop-json-entry; empty for hub and repair-backup"`
	Path       string `json:"path" yaml:"path" table:"path" jsonschema:"description=Worktree or hub or backup path the entry pointed at"`
	Reason     string `json:"reason,omitempty" yaml:"reason,omitempty" jsonschema:"description=Why a skipped entry was left in place (a worktree git has locked, with git's lock reason); absent otherwise"`
}

// envGCRecord is one orphaned dependency directory `git hop env gc`
// deleted or, with --dry-run, would delete.
type envGCRecord struct {
	Action   string `json:"action" yaml:"action" table:"action" jsonschema:"enum=deleted,enum=would-delete,description=deleted; would-delete under --dry-run"`
	Key      string `json:"key" yaml:"key" table:"key" jsonschema:"description=Dependency key (<deps dir>.<lockfile hash>)"`
	Size     int64  `json:"size" yaml:"size" table:"size" jsonschema:"description=Size in bytes"`
	LastUsed string `json:"last_used" yaml:"last_used" table:"last_used" jsonschema:"description=When a worktree last used it (RFC 3339 in UTC); empty when unknown"`
	Path     string `json:"path" yaml:"path" table:"path" jsonschema:"description=Absolute path of the dependency directory"`
}

// repairRecord is one action of the `git hop repair` plan. The columnar
// formats (--porcelain included) carry status, path, kind, old and new in
// that order, the line format repair --porcelain has always printed.
type repairRecord struct {
	Status string `json:"status" yaml:"status" table:"status" jsonschema:"enum=ok,enum=repaired,description=repaired when the plan changes this worktree (applied unless --dry-run); ok when it needs nothing"`
	Path   string `json:"path" yaml:"path" table:"path" jsonschema:"description=Absolute path of the worktree"`
	Kind   string `json:"kind" yaml:"kind" table:"kind" jsonschema:"description=Action kind: noop/rewrite-gitdir/register/unregister/update-hopjson/record-base/restore-fetch-refspec"`
	Old    string `json:"old" yaml:"old" table:"old" jsonschema:"description=Value before the action when the kind has one"`
	New    string `json:"new" yaml:"new" table:"new" jsonschema:"description=Value after the action when the kind has one"`
	Reason string `json:"reason" yaml:"reason" jsonschema:"description=Why the planner chose this action"`
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
	output.RegisterResultShape(cmd, shape)
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
func newAddResult(g git.GitInterface, hub *hop.Hub, branch, worktreePath string, created bool, ports *config.BranchPorts, envStarted bool) addResult {
	res := addResult{
		Branch:     branch,
		Path:       worktreePath,
		Base:       hub.Config.Repo.DefaultBranch,
		Upstream:   branchUpstream(g, worktreePath),
		Created:    created,
		EnvStarted: envStarted,
	}
	if b, ok := hub.Config.Branches[branch]; ok {
		res.Base = resolveCompareBranch(hub.Config, b)
		res.Task = b.Task
	}
	if ports != nil && len(ports.Ports) > 0 {
		res.Ports = ports.Ports
	}
	return res
}
