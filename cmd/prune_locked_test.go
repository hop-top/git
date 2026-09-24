package cmd

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/state"
	"hop.top/git/test/mocks"
)

// prune leaves a worktree git has locked alone, like `git worktree
// prune` does: its directory may only be unavailable (a drive that is not
// mounted). Its state entry and hop.json row survive, and prune reports
// each one it skipped, with the reason.

// TestPrune_SkipsLockedMissingWorktree: usb is missing and locked, gone
// is missing and not. Only gone is pruned, in a preview as in a real run.
func TestPrune_SkipsLockedMissingWorktree(t *testing.T) {
	for _, dryRun := range []bool{true, false} {
		name := "apply"
		if dryRun {
			name = "dry-run"
		}
		t.Run(name, func(t *testing.T) {
			isolateDoctorPaths(t)
			fs := afero.NewMemMapFs()
			hubPath := "/hubs/repo"
			writePruneHub(t, fs, hubPath, []string{"main", "usb", "gone"}, []string{"main"})
			usb, gone := worktreeDir(hubPath, "usb"), worktreeDir(hubPath, "gone")

			st := stateWithHub(hubPath)
			repo := st.Repositories["github.com/test/repo"]
			repo.Worktrees = map[string]*state.WorktreeState{
				"usb":  {Path: usb, Type: "linked", HubPath: hubPath},
				"gone": {Path: gone, Type: "linked", HubPath: hubPath},
			}
			g := mocks.NewMockGit()
			g.WorktreeListOut = porcelainEntry(usb, "usb", "locked on the usb drive") +
				porcelainEntry(gone, "gone", "prunable gitdir file points to non-existent location")

			counts := runPruneAll(fs, g, st, dryRun)

			assert.Equal(t, 1, counts.worktrees, "only the unlocked state entry: %+v", counts.records)
			assert.Equal(t, 1, counts.hopJSONEntries, "only the unlocked hop.json row: %+v", counts.records)

			var skipped []pruneRecord
			for _, rec := range counts.records {
				if rec.Branch == "usb" {
					skipped = append(skipped, rec)
				}
			}
			require.Len(t, skipped, 2, "the hop.json row and the state entry are each reported: %+v", counts.records)
			kinds := []string{}
			for _, rec := range skipped {
				assert.Equal(t, pruneActionSkipped, rec.Action)
				assert.Contains(t, rec.Reason, "locked")
				assert.Contains(t, rec.Reason, "on the usb drive", "the record names git's lock reason")
				assert.Equal(t, usb, rec.Path)
				kinds = append(kinds, rec.Kind)
			}
			assert.ElementsMatch(t, []string{pruneKindHopJSONEntry, pruneKindWorktree}, kinds)

			assert.Contains(t, repo.Worktrees, "usb", "the locked state entry is kept")
			assert.Contains(t, hubBranchKeys(t, fs, hubPath), "usb", "the locked hop.json row is kept")
			if !dryRun {
				assert.NotContains(t, repo.Worktrees, "gone")
				assert.NotContains(t, hubBranchKeys(t, fs, hubPath), "gone")
			}
		})
	}
}

// TestPrune_UnreadableRegistry_Prunes guards the inverse direction: with
// no lock to go by (git's registry cannot be read), prune behaves as it
// always has.
func TestPrune_UnreadableRegistry_Prunes(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	hubPath := "/hubs/repo"
	writePruneHub(t, fs, hubPath, []string{"main", "gone"}, []string{"main"})
	st := stateWithHub(hubPath)
	st.Repositories["github.com/test/repo"].Worktrees = map[string]*state.WorktreeState{
		"gone": {Path: worktreeDir(hubPath, "gone"), Type: "linked", HubPath: hubPath},
	}
	g := mocks.NewMockGit()
	g.WorktreeListErr = assert.AnError

	counts := runPruneAll(fs, g, st, false)

	assert.Equal(t, 1, counts.worktrees)
	assert.Equal(t, 1, counts.hopJSONEntries)
}
