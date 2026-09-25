package cmd

import (
	"path/filepath"

	"github.com/spf13/afero"

	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
	"hop.top/git/internal/state"
)

// Kinds of record prune drops from a hopspace several --global hubs
// share, for a hub whose directory is gone.
const (
	// pruneKindHopspaceRecord is a worktree record in the hopspace's
	// hop.json.
	pruneKindHopspaceRecord = "hopspace-record"
	// pruneKindPortsEntry is a worktree's entry in its ports.json.
	pruneKindPortsEntry = "ports-entry"
	// pruneKindVolumesEntry is a worktree's entry in its volumes.json.
	pruneKindVolumesEntry = "volumes-entry"
)

// pruneGoneHubRecords drops what the hopspaces --global hubs share still
// record for each hub of st whose directory is gone (the hubs
// pruneOrphanedHubs takes out of state): its worktree records in the
// hopspace's hop.json, then its ports.json and volumes.json entries.
// Only the records go: the volume directories they name are left, data
// and all. Under dryRun nothing is written.
//
// It runs before pruneOrphanedHubs, while st still has the hubs and the
// repository they belong to.
func pruneGoneHubRecords(fs afero.Fs, st *state.State, dryRun bool) []pruneRecord {
	var out []pruneRecord
	for _, repoID := range scopeRepoIDs(st) {
		repo := st.Repositories[repoID]
		for _, hub := range repo.Hubs {
			if hub == nil || dirExists(fs, hub.Path) {
				continue
			}
			for _, hs := range sharedHopspacesOf(fs, repo, hub.Path) {
				out = append(out, pruneHubHopspaceRecords(fs, repoID, hs, hub.Path, dryRun)...)
				out = append(out, pruneHubEnvEntries(fs, repoID, hs, hub.Path, dryRun)...)
			}
		}
	}
	return out
}

// sharedHopspacesOf returns the existing hopspaces, shared by --global
// hubs, that may record worktrees of repo's hub at hubPath, whose
// directory is gone: the data-home hopspace of the repository, and the
// one each of its other hubs uses when that is not the hub itself (they
// differ only when hop.dataLayout does).
func sharedHopspacesOf(fs afero.Fs, repo *state.RepositoryState, hubPath string) []string {
	candidates := []string{hop.GetHopspacePath(hop.GetGitHopDataHome(), hop.NewRepoRef(repo.URI, repo.Org, repo.Repo).In(hubPath))}
	for _, h := range repo.Hubs {
		if h == nil || state.SamePath(h.Path, hubPath) {
			continue
		}
		if other, err := hop.LoadHub(fs, h.Path); err == nil {
			candidates = append(candidates, hop.ResolveHopspacePath(h.Path, other.Config.Repo))
		}
	}
	var out []string
	for _, path := range candidates {
		if !hop.SharedHopspace(path, hubPath) || !dirExists(fs, path) || containsPath(out, path) {
			continue
		}
		out = append(out, path)
	}
	return out
}

func containsPath(paths []string, path string) bool {
	for _, p := range paths {
		if state.SamePath(p, path) {
			return true
		}
	}
	return false
}

// pruneHubHopspaceRecords drops the records of the hub at hubPath from
// the hop.json of the shared hopspace at path, deciding again under its
// lock (Hopspace.DropHubRecords). A failure is a warning: doctor reports
// the records left.
func pruneHubHopspaceRecords(fs afero.Fs, repoID, path, hubPath string, dryRun bool) []pruneRecord {
	hs, err := hop.LoadHopspace(fs, path)
	if err != nil {
		return nil
	}
	keys := hs.HubRecords(hubPath)
	if len(keys) == 0 {
		return nil
	}
	branches := make(map[string]string, len(keys))
	for _, k := range keys {
		branches[k] = hs.Config.Branches[k].Branch
	}
	hopJSON := filepath.Join(path, "hop.json")
	if !dryRun {
		if keys, err = hs.DropHubRecords(hubPath); err != nil {
			output.Warn("Failed to drop the records of hub %s from %s: %v", hubPath, hopJSON, err)
			return nil
		}
	}
	prefix := "Pruning"
	if dryRun {
		prefix = "[dry-run] Would prune"
	}
	recs := make([]pruneRecord, 0, len(keys))
	for _, k := range keys {
		output.Info("%s hopspace record: %s:%s (%s) from %s", prefix, repoID, branches[k], k, hopJSON)
		recs = append(recs, newPruneRecord(pruneKindHopspaceRecord, repoID, branches[k], k, dryRun))
	}
	return recs
}

// pruneHubEnvEntries drops the ports.json and volumes.json entries of the
// hub at hubPath from the shared hopspace at path
// (services.DropHubEnvEntries), leaving the volume directories alone. A
// failure is a warning.
func pruneHubEnvEntries(fs afero.Fs, repoID, path, hubPath string, dryRun bool) []pruneRecord {
	entries := services.HubEnvEntries(fs, path, hubPath)
	if len(entries) == 0 {
		return nil
	}
	if !dryRun {
		var err error
		entries, err = services.DropHubEnvEntries(fs, path, hubPath)
		if err != nil {
			output.Warn("Failed to drop the entries of hub %s from the ports and volumes of %s: %v", hubPath, path, err)
		}
	}
	prefix := "Pruning"
	if dryRun {
		prefix = "[dry-run] Would prune"
	}
	var recs []pruneRecord
	for _, e := range entries {
		output.Info("%s ports entry: %s:%s (%s) from %s", prefix, repoID, e.Branch, e.Key, filepath.Join(path, "ports.json"))
		recs = append(recs, newPruneRecord(pruneKindPortsEntry, repoID, e.Branch, e.Key, dryRun))
	}
	for _, e := range entries {
		if e.Volumes {
			output.Info("%s volumes entry: %s:%s (%s) from %s", prefix, repoID, e.Branch, e.Key, filepath.Join(path, "volumes.json"))
			recs = append(recs, newPruneRecord(pruneKindVolumesEntry, repoID, e.Branch, e.Key, dryRun))
		}
	}
	return recs
}
