package cmd

import (
	"path/filepath"

	"github.com/spf13/afero"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// A save of state.json or hop.json writes a temp file next to it and
// renames it over the file; a run that dies in between leaves the temp
// file behind. prune sweeps those unmodified for filelock.StaleTempAge,
// holding the saved file's lock, and only files named exactly as the
// save names them (state.IsTempName, config.IsTempName).

// pruneStateTemps sweeps the temp files left next to state.json. They
// belong to no one repository, so only prune --all runs it. A held state
// lock (a save running now) leaves them for a later prune, with a
// warning.
func pruneStateTemps(fs afero.Fs, dryRun bool) []pruneRecord {
	paths, err := state.SweepTemps(fs, dryRun)
	return tempRecords("", paths, err, dryRun)
}

// pruneHubTemps sweeps the temp files left next to the hop.json of each
// hub in scope, and of its hopspace when that lies elsewhere (a --global
// hub's, in the data home, where ports.json and volumes.json live too).
// A directory without a hop.json is left alone, and a hopspace several
// hubs share is swept once.
func pruneHubTemps(fs afero.Fs, st *state.State, dryRun bool) []pruneRecord {
	var pruned []pruneRecord
	seen := map[string]bool{}
	for _, h := range hubPathsFromState(st) {
		hub, err := hop.LoadHub(fs, h.path)
		if err != nil {
			continue
		}
		for _, dir := range []string{h.path, hop.ResolveHopspacePath(h.path, hub.Config.Repo)} {
			key := state.ResolvePath(dir)
			if seen[key] {
				continue
			}
			seen[key] = true
			if ok, _ := afero.Exists(fs, filepath.Join(dir, "hop.json")); !ok {
				continue
			}
			paths, err := hop.SweepHopJSONTemps(fs, dir, dryRun)
			pruned = append(pruned, tempRecords(h.repoID, paths, err, dryRun)...)
		}
	}
	return pruned
}

// tempRecords reports the temp files a sweep removed (or would remove)
// and returns their records; err, a held lock or a file that could not
// be removed, is a warning.
func tempRecords(repoID string, paths []string, err error, dryRun bool) []pruneRecord {
	prefix := "Pruning"
	if dryRun {
		prefix = "[dry-run] Would prune"
	}
	records := make([]pruneRecord, 0, len(paths))
	for _, path := range paths {
		if repoID == "" {
			output.Info("%s temp file: %s", prefix, path)
		} else {
			output.Info("%s temp file: %s (%s)", prefix, repoID, path)
		}
		records = append(records, newPruneRecord(pruneKindTempFile, repoID, "", path, dryRun))
	}
	if err != nil {
		output.Warn("stale temp files left in place: %v", err)
	}
	return records
}
