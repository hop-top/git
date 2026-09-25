package cmd

import (
	"path/filepath"

	"github.com/spf13/afero"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
	"hop.top/git/internal/state"
)

// unlinkedLegacyDepsStores returns the deps stores earlier releases wrote
// that no worktree of any hub links into (services.FindLegacyDepsStores).
// Two hubs could share one such store, so the scope is every hub state
// records, plus hubPath (when not "") in case it is not recorded. An
// unreadable state is an error: without it no store can be shown
// unlinked.
func unlinkedLegacyDepsStores(fs afero.Fs, hubPath string) ([]services.LegacyDepsStore, error) {
	st, err := state.LoadState(fs)
	if err != nil {
		return nil, err
	}

	var hopspaces, worktrees []string
	addHub := func(path, mode string, ref hop.RepoRef) {
		if hub, err := hop.LoadHub(fs, path); err == nil {
			hopspaces = append(hopspaces, hop.ResolveHopspacePath(path, hub.Config.Repo))
			for _, wt := range hub.WorktreePaths() {
				worktrees = append(worktrees, wt)
			}
			return
		}
		if mode == state.HubModeGlobal {
			hopspaces = append(hopspaces, hop.GetHopspacePath(hop.GetGitHopDataHome(), ref))
			return
		}
		hopspaces = append(hopspaces, path)
	}
	for _, repo := range st.Repositories {
		for _, wt := range repo.Worktrees {
			worktrees = append(worktrees, wt.Path)
		}
		for _, h := range repo.Hubs {
			addHub(h.Path, h.Mode, hop.NewRepoRef(repo.URI, repo.Org, repo.Repo))
		}
	}
	if hubPath != "" {
		addHub(hubPath, "", hop.RepoRef{})
	}

	stores, err := services.FindLegacyDepsStores(fs, hopspaces, worktrees)
	if err != nil {
		return nil, err
	}
	unlinked := stores[:0]
	for _, s := range stores {
		if !s.Linked() {
			unlinked = append(unlinked, s)
		}
	}
	return unlinked, nil
}

// legacyStoreKey names a legacy store in results: its path under the data
// home, e.g. "git/deps".
func legacyStoreKey(path string) string {
	if rel, err := filepath.Rel(hop.GetGitHopDataHome(), path); err == nil {
		return rel
	}
	return path
}

// checkLegacyDepsStores reports the old deps stores nothing links into.
// Read-only, as for orphaned deps: removing them is 'git hop env gc'.
func checkLegacyDepsStores(fs afero.Fs, hubPath string, r *doctorReport) {
	stores, err := unlinkedLegacyDepsStores(fs, hubPath)
	if err != nil {
		output.Warn("Cannot check old dependency stores: %v", err)
		r.record(doctorKindWarning, doctorCheckDependencies, hop.GetGitHopDataHome(),
			"cannot check old dependency stores: %v", err)
		return
	}
	if len(stores) == 0 {
		return
	}
	var total int64
	for _, s := range stores {
		total += s.Size
	}
	output.Info("\n  %d old dependency store(s) no worktree links to (%.1fMB):", len(stores), mb(total))
	for _, s := range stores {
		output.Info("    %s  ~%.1fMB", s.Path, mb(s.Size))
		r.record(doctorKindWarning, doctorCheckDependencies, s.Path,
			"old dependency store no worktree links to (%.1fMB); run 'git hop env gc' to remove it", mb(s.Size))
	}
	output.Info("    Run 'git hop env gc' to remove them")
}

func mb(bytes int64) float64 { return float64(bytes) / 1024 / 1024 }

// legacyGCRecords describes stores as would-delete records.
func legacyGCRecords(stores []services.LegacyDepsStore) []envGCRecord {
	records := make([]envGCRecord, 0, len(stores))
	for _, s := range stores {
		records = append(records, envGCRecord{
			Action: "would-delete",
			Key:    legacyStoreKey(s.Path),
			Size:   s.Size,
			Path:   s.Path,
		})
	}
	return records
}

// removeLegacyDepsStores removes the stores of preview that a fresh scan
// still finds unlinked, so a link made since the preview keeps its store,
// and returns their records marked deleted.
func removeLegacyDepsStores(fs afero.Fs, hubPath string, preview []services.LegacyDepsStore) []envGCRecord {
	if len(preview) == 0 {
		return nil
	}
	fresh, err := unlinkedLegacyDepsStores(fs, hubPath)
	if err != nil {
		output.Warn("Cannot check old dependency stores, keeping them: %v", err)
		return nil
	}
	previewed := make(map[string]bool, len(preview))
	for _, s := range preview {
		previewed[s.Path] = true
	}
	var removed []services.LegacyDepsStore
	for _, s := range fresh {
		if !previewed[s.Path] {
			continue
		}
		if err := services.RemoveLegacyDepsStore(fs, s); err != nil {
			output.Error("Failed to remove old dependency store %s: %v", s.Path, err)
			continue
		}
		removed = append(removed, s)
	}
	records := legacyGCRecords(removed)
	for i := range records {
		records[i].Action = "deleted"
	}
	return records
}
