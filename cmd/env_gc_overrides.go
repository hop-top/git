package cmd

import (
	"path/filepath"

	"github.com/spf13/afero"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
)

// orphanedOverrideDirs returns the compose override cache directories no
// worktree of any hub uses (services.OrphanedOverrideDirs). Hubs of one
// repository share its cache directory, so the scope is every hub state
// records, plus hubPath; an unreadable state is an error.
func orphanedOverrideDirs(fs afero.Fs, hubPath string) ([]services.OverrideDir, error) {
	recs, err := services.LoadEnvRecords(fs, hubPath)
	if err != nil {
		return nil, err
	}
	return services.OrphanedOverrideDirs(fs, recs)
}

// overrideGCKey names an override directory in results: its path under
// the cache directory, e.g. "org/repo/app-1a2b3c4d/main".
func overrideGCKey(path string) string {
	if rel, err := filepath.Rel(hop.GetGitHopCacheHome(), path); err == nil {
		return rel
	}
	return path
}

// overrideGCRecords describes dirs as would-delete records.
func overrideGCRecords(dirs []services.OverrideDir) []envGCRecord {
	records := make([]envGCRecord, 0, len(dirs))
	for _, d := range dirs {
		records = append(records, envGCRecord{Action: "would-delete", Key: overrideGCKey(d.Path), Size: d.Size, Path: d.Path})
	}
	return records
}

// removeOverrideDirs removes the directories of preview that a fresh scan
// still finds unused, and returns their records marked deleted.
func removeOverrideDirs(fs afero.Fs, hubPath string, preview []services.OverrideDir) []envGCRecord {
	if len(preview) == 0 {
		return nil
	}
	fresh, err := orphanedOverrideDirs(fs, hubPath)
	if err != nil {
		output.Warn("Cannot check compose override caches, keeping them: %v", err)
		return nil
	}
	previewed := make(map[string]bool, len(preview))
	for _, d := range preview {
		previewed[d.Path] = true
	}
	var removed []services.OverrideDir
	for _, d := range fresh {
		if !previewed[d.Path] {
			continue
		}
		if err := services.RemoveOverrideDir(fs, d); err != nil {
			output.Error("Failed to remove compose override %s: %v", d.Path, err)
			continue
		}
		removed = append(removed, d)
	}
	records := overrideGCRecords(removed)
	for i := range records {
		records[i].Action = "deleted"
	}
	return records
}
