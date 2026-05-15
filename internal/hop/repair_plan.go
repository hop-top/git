package hop

// ActionKind enumerates the corrective actions the repair planner can emit.
// Each kind has a corresponding applier function in repair_apply.go.
type ActionKind int

const (
	// ActionNoOp marks a worktree that is already healthy. Skipped during apply.
	ActionNoOp ActionKind = iota
	// ActionRewriteGitdir corrects a stale .git/worktrees/<name>/gitdir pointer
	// (typically after a worktree directory was moved on disk).
	ActionRewriteGitdir
	// ActionRegisterWithGit re-adds a worktree to git's registry when the
	// directory exists on disk and in hop.json but is missing from
	// `git worktree list --porcelain`.
	ActionRegisterWithGit
	// ActionUnregisterFromGit prunes git's registry of an entry whose
	// worktree directory has been deleted.
	ActionUnregisterFromGit
	// ActionUpdateHopJSON realigns hop.json with on-disk + git-registry reality
	// (e.g. branch entry's path is wrong).
	ActionUpdateHopJSON
	// ActionRecordBase backfills HubBranch.Base on a legacy branch entry
	// using local inference signals (branch.<name>.merge config first;
	// most-recent merge-base across known branches as fallback). Emitted
	// only when the planner is invoked with the base-inference flag set;
	// applies idempotently — branches with Base already set are skipped.
	ActionRecordBase
	// ActionRestoreFetchRefspec re-adds the standard
	//   +refs/heads/*:refs/remotes/origin/*
	// fetch refspec to a bare repo's [remote "origin"] config when it
	// is missing. `git clone --bare` strips the refspec, and any hub
	// cloned before commit cc5def4 (May 3, 2026) inherits the defect:
	// `git fetch origin` lands in FETCH_HEAD without populating
	// refs/remotes/origin/*, which breaks every downstream call that
	// asks "does origin/<branch> still exist". Emitted hub-wide (not
	// per-branch), only when pathspec is empty — the defect is in the
	// hub config, not a worktree.
	ActionRestoreFetchRefspec
)

// String returns the porcelain action token (kebab-case) for stable output.
func (k ActionKind) String() string {
	switch k {
	case ActionNoOp:
		return "noop"
	case ActionRewriteGitdir:
		return "rewrite-gitdir"
	case ActionRegisterWithGit:
		return "register"
	case ActionUnregisterFromGit:
		return "unregister"
	case ActionUpdateHopJSON:
		return "update-hopjson"
	case ActionRecordBase:
		return "record-base"
	case ActionRestoreFetchRefspec:
		return "restore-fetch-refspec"
	default:
		return "unknown"
	}
}

// Action is one corrective step the planner emits.
// OldValue/NewValue carry kind-specific context (e.g. for RewriteGitdir
// they hold the stale and correct gitdir paths). Reason is a short
// human-readable classification surfaced in plan tables.
type Action struct {
	Kind         ActionKind
	WorktreePath string
	OldValue     string
	NewValue     string
	Reason       string
}

// Plan is the full set of actions required to bring a hub's worktrees back
// to a consistent state. Actions are applied in slice order.
//
// Warnings record advisory issues that don't translate to actions (e.g.
// dirty worktrees the operator must resolve before running without
// --force-dirty, or ambiguous classifications the planner could not
// resolve unilaterally).
type Plan struct {
	HubPath  string
	Actions  []Action
	Warnings []string
}

// HasMutations returns true when the plan contains at least one Action
// other than NoOp. Used by the command to short-circuit dry-run output
// when there's nothing to do.
func (p *Plan) HasMutations() bool {
	for _, a := range p.Actions {
		if a.Kind != ActionNoOp {
			return true
		}
	}
	return false
}
