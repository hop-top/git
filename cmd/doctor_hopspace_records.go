package cmd

import (
	"path/filepath"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// checkGoneHubRecords warns about the records a data-home hopspace
// several --global hubs share holds for worktrees of a hub that no longer
// exists: state does not record it and its directory is gone (deleted by
// hand, then taken out of state by an earlier release's doctor --fix or
// prune; both now drop the records with the hub, dropGoneHubRecords).
// Nothing reads them, so they are warnings; --fix drops them. A hub's own
// hopspace has no such records: it goes with the hub.
func checkGoneHubRecords(fs afero.Fs, hubPath, hopspacePath string, opts doctorOpts, r *doctorReport) {
	if !hop.SharedHopspace(hopspacePath, hubPath) {
		return
	}
	hs, err := hop.LoadHopspace(fs, hopspacePath)
	if err != nil {
		// Reported by reconcileHopspaceBranches.
		return
	}
	st, err := state.LoadState(fs)
	if err != nil {
		// Which hubs exist is unknown; the state check reports why.
		return
	}
	gone := func(_ string, e config.HopspaceBranch) bool {
		return e.Hub != "" && !hubInState(st, e.Hub) && !dirExists(fs, e.Hub)
	}
	keys := hs.Records(gone)
	if len(keys) == 0 {
		return
	}

	hopJSON := filepath.Join(hopspacePath, "hop.json")
	for _, k := range keys {
		const msg = "hopspace record of %s belongs to hub %s, which no longer exists"
		hub := hs.Config.Branches[k].Hub
		output.Warn(msg, k, hub)
		r.record(doctorKindWarning, doctorCheckHopspace, k, msg+"; run 'git hop doctor --fix' to drop it", k, hub)
	}
	switch {
	case !opts.fix:
		output.Hint("run 'git hop doctor --fix' to drop them from %s", hopJSON)
	case !opts.mutating():
		for _, k := range keys {
			output.Info("[dry-run] Would drop hopspace record of %s from %s", k, hopJSON)
			r.repaired(opts, doctorCheckHopspace, k, "drop from %s", hopJSON)
		}
	default:
		// Decided again on hop.json as it is under its lock.
		dropped, err := hs.DropRecords(hubPath, gone)
		if err != nil {
			output.Error("Failed to drop hopspace records from %s: %v", hopJSON, err)
			for _, k := range keys {
				r.failed(doctorCheckHopspace, k, "drop from %s: %v", hopJSON, err)
			}
			return
		}
		for _, k := range dropped {
			output.Info("Dropped hopspace record of %s from %s", k, hopJSON)
			r.repaired(opts, doctorCheckHopspace, k, "drop from %s", hopJSON)
		}
	}
}

// hubInState reports whether st records a hub at path, in any repository.
func hubInState(st *state.State, path string) bool {
	for _, repo := range st.Repositories {
		for _, h := range repo.Hubs {
			if h != nil && state.SamePath(h.Path, path) {
				return true
			}
		}
	}
	return false
}

func dirExists(fs afero.Fs, path string) bool {
	ok, _ := afero.DirExists(fs, path)
	return ok
}
