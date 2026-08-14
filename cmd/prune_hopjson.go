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
// worktree directory no longer exists on disk, and returns the count
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
// hop.RepairBackup taking the pre-mutation snapshot into
// <hub>/.hop/backups/repair-<ts>Z. A prune that removes rows is
// therefore undoable via `git hop repair --undo <id>` exactly like a
// repair.
//
// Hubs are visited in a stable order and each is handled independently:
// one unreadable or non-hub entry does not abort the others.
func pruneOrphanedHubBranches(fs afero.Fs, g git.GitInterface, st *state.State, dryRun bool) int {
	prefix := "Pruning"
	if dryRun {
		prefix = "[dry-run] Would prune"
	}

	pruned := 0
	for _, hubPath := range hubPathsFromState(st) {
		hub, err := hop.LoadHub(fs, hubPath)
		if err != nil {
			// State references a path that is no longer a hub (or was
			// never one). pruneOrphanedHubs handles the fully-missing
			// case; here we simply have nothing to rewrite.
			continue
		}

		plan := &hop.Plan{HubPath: hubPath}
		for _, branch := range sortedBranchNames(hub) {
			wtPath := hub.BranchPath(branch)
			if exists, _ := afero.DirExists(fs, wtPath); exists {
				continue
			}
			output.Info("%s hop.json entry: %s (%s)", prefix, branch, wtPath)
			plan.Actions = append(plan.Actions, hop.Action{
				Kind:         hop.ActionUpdateHopJSON,
				WorktreePath: wtPath,
				Reason:       "hop.json references missing path for branch " + branch,
			})
		}
		if len(plan.Actions) == 0 {
			continue
		}
		if dryRun {
			pruned += len(plan.Actions)
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
		// Count what actually landed, not what was planned — the
		// "Pruned N" line must describe the post-state.
		pruned += mutations
	}

	return pruned
}

// stateScopedToHub returns a state view containing only hubPath, so a
// caller can drive pruneOrphanedHubBranches against a single hub instead
// of every hub in global state.
//
// prune is a global command and rightly visits them all; doctor runs
// from one hub and must not rewrite a sibling repo's hop.json. Returns
// nil when hubPath is empty (not in a hub) — nothing to scope to.
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

// hubPathsFromState returns every hub path in state, deduplicated and
// sorted. Multiple repositories can register the same hub path; visiting
// it twice would snapshot and rewrite hop.json twice.
func hubPathsFromState(st *state.State) []string {
	seen := make(map[string]struct{})
	var paths []string
	for _, repo := range st.Repositories {
		for _, hub := range repo.Hubs {
			if hub == nil || hub.Path == "" {
				continue
			}
			if _, dup := seen[hub.Path]; dup {
				continue
			}
			seen[hub.Path] = struct{}{}
			paths = append(paths, hub.Path)
		}
	}
	sort.Strings(paths)
	return paths
}

func sortedBranchNames(hub *hop.Hub) []string {
	names := make([]string, 0, len(hub.Config.Branches))
	for name := range hub.Config.Branches {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
