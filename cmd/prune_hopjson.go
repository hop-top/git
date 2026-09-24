package cmd

import (
	"sort"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// pruneOrphanedHubBranches drops hub hop.json branch entries whose
// worktree directory no longer exists on disk, and returns the entries
// removed (or that would be removed under dryRun).
//
// This is the hub-local half of prune. The state half (runPruneFS)
// rewrites the global state.json, but `git hop status` renders its
// hub table from hop.json — so pruning state alone left every deleted
// worktree listed as `Missing` forever, and the "Pruned N" line was a
// claim about a file the user never reads.
//
// Mechanism is repair's, not a second implementation: the same
// ActionUpdateHopJSON kind the planner emits for a hop.json entry
// pointing at a missing path, applied through hop.Applier, with
// hop.RepairBackup taking the pre-mutation snapshot into the per-hub
// repair state dir (hop.RepairBackupRoot). A prune that removes rows is
// therefore undoable via `git hop repair --undo <id>` exactly like a
// repair.
//
// Hubs are visited in a stable order and each is handled independently:
// one unreadable or non-hub entry does not abort the others.
//
// A row whose worktree git has locked is kept and reported skipped
// (skipLockedEntry): like `git worktree prune`, prune leaves it for the
// user to unlock. The returned records include the skipped rows.
func pruneOrphanedHubBranches(fs afero.Fs, g git.GitInterface, st *state.State, dryRun bool) []pruneRecord {
	return pruneHubBranchesKeeping(fs, g, st, dryRun, nil)
}

// pruneHubBranchesKeeping is pruneOrphanedHubBranches also leaving alone
// every row whose worktree path keep reports true for. A nil keep keeps
// nothing beyond the locked rows.
func pruneHubBranchesKeeping(fs afero.Fs, g git.GitInterface, st *state.State, dryRun bool, keep func(path string) bool) []pruneRecord {
	prefix := "Pruning"
	if dryRun {
		prefix = "[dry-run] Would prune"
	}

	var pruned []pruneRecord
	for _, h := range hubPathsFromState(st) {
		hubPath := h.path
		hub, err := hop.LoadHub(fs, hubPath)
		if err != nil {
			// State references a path that is no longer a hub (or was
			// never one). pruneOrphanedHubs handles the fully-missing
			// case; here we simply have nothing to rewrite.
			continue
		}

		plan := &hop.Plan{HubPath: hubPath}
		var entries []pruneRecord
		var registry *string // git's worktree list, read once a row is missing
		for _, branch := range sortedBranchNames(hub) {
			wtPath := hub.BranchPath(branch)
			if worktreeDirPresent(fs, wtPath) {
				continue
			}
			if registry == nil {
				list, _ := g.WorktreeListPorcelain(hubPath)
				registry = &list
			}
			if reason, locked := worktreeLock(*registry, wtPath); locked {
				pruned = append(pruned, skipLockedEntry(pruneKindHopJSONEntry, "hop.json entry", h.repoID, branch, wtPath, reason))
				continue
			}
			if keep != nil && keep(wtPath) {
				output.Info("Keeping hop.json entry: %s:%s (%s)", h.repoID, branch, wtPath)
				continue
			}
			output.Info("%s hop.json entry: %s:%s (%s)", prefix, h.repoID, branch, wtPath)
			plan.Actions = append(plan.Actions, hop.Action{
				Kind:         hop.ActionUpdateHopJSON,
				WorktreePath: wtPath,
				Reason:       "hop.json references missing path for branch " + branch,
			})
			entries = append(entries, newPruneRecord(pruneKindHopJSONEntry, h.repoID, branch, wtPath, dryRun))
		}
		if len(plan.Actions) == 0 {
			continue
		}
		if dryRun {
			pruned = append(pruned, entries...)
			continue
		}

		if _, err := hop.NewRepairBackup(fs, hubPath).Snapshot(plan); err != nil {
			output.Error("Failed to back up %s/hop.json, skipping: %v", hubPath, err)
			continue
		}
		mutations, err := hop.NewApplier(fs, g).Apply(plan)
		if err != nil {
			output.Error("Failed to prune hop.json entries in %s: %v", hubPath, err)
		}
		// Report what actually landed, not what was planned — the
		// "Pruned N" line must describe the post-state. The applier works
		// through the plan in order and stops at the first failure, and
		// every entry it reaches changes hop.json, so the landed entries
		// are the first mutations of them.
		pruned = append(pruned, entries[:mutations]...)
	}

	return pruned
}

// stateScopedToHub returns a state view containing only hubPath, so a
// caller can drive pruneOrphanedHubBranches against a single hub instead
// of every hub in the state it was handed.
//
// Used by doctor, which runs from one hub and must not rewrite a sibling
// repo's hop.json. prune does not use it: prune scopes by repository
// (resolvePruneScope) rather than by hub, because its other passes must
// mutate the real *RepositoryState entries that get saved back to
// state.json — a synthetic single-hub view keyed by path has no repo ID
// and no worktree rows for those passes to act on. Returns nil when
// hubPath is empty (not in a hub) — nothing to scope to.
func stateScopedToHub(hubPath string) *state.State {
	if hubPath == "" {
		return nil
	}
	return &state.State{
		Repositories: map[string]*state.RepositoryState{
			hubPath: {Hubs: []*state.HubState{{Path: hubPath}}},
		},
	}
}

// stateHub pairs a hub path with the repository that registered it, so
// prune's hop.json pass can name the affected repo in its output instead
// of printing a bare path the user has to map back themselves.
type stateHub struct {
	path   string
	repoID string
}

// hubPathsFromState returns every hub in state, deduplicated by path and
// sorted. Multiple repositories can register the same hub path; visiting
// it twice would snapshot and rewrite hop.json twice. On such a
// collision the first repository in sorted order wins the attribution.
func hubPathsFromState(st *state.State) []stateHub {
	repoIDs := make([]string, 0, len(st.Repositories))
	for id := range st.Repositories {
		repoIDs = append(repoIDs, id)
	}
	sort.Strings(repoIDs)

	seen := make(map[string]struct{})
	var hubs []stateHub
	for _, repoID := range repoIDs {
		for _, hub := range st.Repositories[repoID].Hubs {
			if hub == nil || hub.Path == "" {
				continue
			}
			if _, dup := seen[hub.Path]; dup {
				continue
			}
			seen[hub.Path] = struct{}{}
			hubs = append(hubs, stateHub{path: hub.Path, repoID: repoID})
		}
	}
	sort.Slice(hubs, func(i, j int) bool { return hubs[i].path < hubs[j].path })
	return hubs
}

func sortedBranchNames(hub *hop.Hub) []string {
	names := make([]string, 0, len(hub.Config.Branches))
	for name := range hub.Config.Branches {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
