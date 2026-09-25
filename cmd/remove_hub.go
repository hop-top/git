package cmd

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/repoid"
	"hop.top/git/internal/services"
	"hop.top/git/internal/state"
)

// removeHub removes the hub at hubPath: its worktrees, the hub directory
// and its state entry, then the repository's data-home hopspace, or, when
// other hubs keep that, the hub's records in it. It returns one record per
// worktree in branch order, then the hub's, then the hopspace's when there
// is one.
//
// Volume data is kept unless deleteVolumes (--delete-volumes): the
// volume directories in the hub, and in a hopspace removed with it, are
// moved aside to the data home's orphaned-volumes directory first (or,
// when they cannot be moved, left where they are), and a hub's volumes in
// a hopspace other hubs keep stay there.
func removeHub(fs afero.Fs, hubPath string, deleteVolumes bool) []removeRecord {
	output.Info("Removing hub at %s...", hubPath)

	// Load hub to get repo info
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		output.Fatal("Failed to load hub: %v", err)
	}

	repoID := repoid.For(hubPath, hub.Config.Repo)
	ref := hop.RepoRefFor(hubPath, hub.Config.Repo)

	// The volume data goes aside before anything is deleted.
	hubVols := newVolumeMove(fs, hubPath, hubPath, ref, services.HubKey(hubPath), deleteVolumes)
	hubKept, leave := hubVols.moveAside(fs, time.Now())

	// Remove all worktrees
	recs := make([]removeRecord, 0, len(hub.Config.Branches)+2)
	for branchName, branchConfig := range hub.Config.Branches {
		worktreePath := config.ResolveWorktreePath(branchConfig.Path, hubPath)
		output.Info("Removing worktree for branch %s...", branchName)

		rec := removeRecord{Kind: removeKindWorktree, Branch: branchName, Path: worktreePath, Removed: true}
		if err := removeAllExcept(fs, worktreePath, leave); err != nil {
			output.Warn("Failed to remove worktree %s: %v", branchName, err)
			rec.Removed = false
			rec.Reason = fmt.Sprintf("failed to remove worktree: %v", err)
		}
		recs = append(recs, rec)
	}
	sortRemoveRecords(recs)

	// Remove hub directory
	output.Info("Removing hub directory...")
	if err := removeAllExcept(fs, hubPath, leave); err != nil {
		hintKeptVolumes(hubKept)
		output.Fatal("Failed to remove hub directory: %v", err)
	}
	recs = append(recs, volumesRecord(removeRecord{Kind: removeKindHub, Path: hubPath, Removed: true}, hubVols, hubKept))

	// Remove from global state: this hub and its worktrees. The
	// repository's other hubs, and their worktrees, stay.
	// st is state as the removal left it, nil when it could not be
	// loaded (stErr says why).
	var st *state.State
	otherHubs := false
	stErr := state.Update(fs, func(fresh *state.State) error {
		st = fresh
		otherHubs = otherHubsInState(fresh, repoID, hubPath)
		return fresh.RemoveHub(repoID, hubPath)
	})
	if st != nil {
		if stErr != nil {
			output.Warn("Failed to update state: %v", stErr)
		} else {
			output.Info("Removed %s", stateRemoval(repoID, hubPath, otherHubs))
		}
		stErr = nil
	}

	// A default hub's hopspace is the hub directory removed above; a
	// --global hub's is the repository's data-home hopspace, which other
	// hubs of the repository may share.
	d := dataHomeHopspaceFor(fs, st, stErr, hub, hubPath)
	kept := hubKept
	switch {
	case !d.exists:
	case d.remove():
		output.Info("Cleaning up hopspace data...")
		vols := newVolumeMove(fs, d.path, d.path, ref, "hopspace", deleteVolumes)
		hsKept, hsLeave := vols.moveAside(fs, time.Now())
		kept = append(kept, hsKept...)
		rec := volumesRecord(d.record(true), vols, hsKept)
		if err := removeAllExcept(fs, d.path, hsLeave); err != nil {
			output.Warn("Failed to remove hopspace data: %v", err)
			rec.Removed = false
			rec.Reason = fmt.Sprintf("failed to remove hopspace data: %v", err)
		}
		recs = append(recs, rec)
	default:
		output.Info("Keeping hopspace data at %s: %s", d.path, d.reason())
		rec := d.record(false)
		if d.ofHub {
			rec, kept = keepOrDeleteHubVolumes(fs, rec, d.path, hubPath, deleteVolumes, kept)
		}
		dropHubHopspaceRecords(fs, d, hubPath)
		if _, err := services.DropHubEnvEntries(fs, d.path, hubPath); err != nil {
			output.Warn("Failed to update ports and volumes: %v", err)
		}
		output.Hint("it is removed along with the last hub that uses it")
		recs = append(recs, rec)
	}
	hintKeptVolumes(kept)

	output.Success("Successfully removed hub: %s", hubPath)
	return recs
}

// volumesRecord adds to rec what the removal did with the volume data v
// found: kept at kept, or deleted.
func volumesRecord(rec removeRecord, v volumeMove, kept []string) removeRecord {
	rec.Volumes = v.state()
	switch rec.Volumes {
	case volumesKept:
		rec.VolumePaths = kept
	case volumesDeleted:
		rec.VolumePaths = v.paths
	}
	return rec
}

// keepOrDeleteHubVolumes deals with the volumes of the hub at hubPath in
// the hopspace at hopspacePath, which other hubs keep: they stay, unless
// deleteVolumes. It returns rec with what was done, and kept with the
// directory added when it stays.
func keepOrDeleteHubVolumes(fs afero.Fs, rec removeRecord, hopspacePath, hubPath string, deleteVolumes bool, kept []string) (removeRecord, []string) {
	dir := hubVolumesIn(fs, hopspacePath, hubPath)
	if dir == "" {
		return rec, kept
	}
	rec.VolumePaths = []string{dir}
	if !deleteVolumes {
		rec.Volumes = volumesKept
		return rec, append(kept, dir)
	}
	if err := fs.RemoveAll(dir); err != nil {
		output.Warn("Failed to delete volume data %s: %v", dir, err)
		rec.Volumes = volumesKept
		return rec, append(kept, dir)
	}
	output.Info("Deleted volume data %s", dir)
	rec.Volumes = volumesDeleted
	return rec, kept
}

// dropHubHopspaceRecords removes the worktree records of the hub removed
// at hubPath from the data-home hopspace d, kept for the other hubs that
// share it. A failure is a warning: the hub is gone either way, and
// doctor reports records whose hub no longer exists.
func dropHubHopspaceRecords(fs afero.Fs, d dataHomeHopspace, hubPath string) {
	if !d.ofHub {
		return
	}
	hs, err := hop.LoadHopspace(fs, d.path)
	if err == nil {
		var keys []string
		keys, err = hs.DropHubRecords(hubPath)
		if len(keys) > 0 {
			output.Info("Dropped %d record(s) of hub %s from %s", len(keys), hubPath, filepath.Join(d.path, "hop.json"))
		}
	}
	if err != nil {
		output.Warn("Failed to drop the hub's records from %s: %v", filepath.Join(d.path, "hop.json"), err)
	}
}

// previewHubHopspaceRecords says which records dropHubHopspaceRecords
// would remove.
func previewHubHopspaceRecords(fs afero.Fs, d dataHomeHopspace, hubPath string) {
	if !d.ofHub {
		return
	}
	hs, err := hop.LoadHopspace(fs, d.path)
	if err != nil {
		return
	}
	if keys := hs.HubRecords(hubPath); len(keys) > 0 {
		output.Info("[dry-run] Would drop %d record(s) of hub %s from %s", len(keys), hubPath, filepath.Join(d.path, "hop.json"))
	}
}

// otherHubsInState reports whether state records a hub of repoID other
// than the one at hubPath, i.e. whether the repository stays in state
// once that hub is removed.
func otherHubsInState(st *state.State, repoID, hubPath string) bool {
	repo, ok := st.Repositories[repoID]
	if !ok {
		return false
	}
	for _, h := range repo.Hubs {
		if h != nil && !state.SamePath(h.Path, hubPath) {
			return true
		}
	}
	return false
}

// stateRemoval says what removing the hub at hubPath takes out of state:
// the whole repository, or only this hub when others stay.
func stateRemoval(repoID, hubPath string, otherHubs bool) string {
	if otherHubs {
		return fmt.Sprintf("hub %s from state ('%s' keeps its other hubs)", hubPath, repoID)
	}
	return fmt.Sprintf("'%s' from state", repoID)
}

// dataHomeHopspace is what removing a hub does to its repository's
// data-home hopspace ($GIT_HOP_DATA_HOME/<org>/<repo>, where clone
// --global keeps it).
type dataHomeHopspace struct {
	path   string
	exists bool
	// usedBy lists the other hubs state records that still use it.
	usedBy []string
	// stateErr is set when state could not be read: which hubs use the
	// hopspace is then unknown, and it is kept.
	stateErr error
	// ofHub reports whether the hub removed uses it (is marked global),
	// i.e. whether it records that hub's worktrees.
	ofHub bool
}

// remove reports whether the hopspace goes with the hub: it is there and
// no other hub is known to use it.
func (d dataHomeHopspace) remove() bool {
	return d.exists && d.stateErr == nil && len(d.usedBy) == 0
}

// reason says why the hopspace is kept, or why it goes.
func (d dataHomeHopspace) reason() string {
	switch {
	case d.stateErr != nil:
		return fmt.Sprintf("cannot tell whether another hub uses it (%v)", d.stateErr)
	case len(d.usedBy) > 0:
		return "still used by " + strings.Join(d.usedBy, ", ")
	default:
		return "no other hub uses it"
	}
}

// record is the hopspace's remove record: removed, or kept and why.
func (d dataHomeHopspace) record(removed bool) removeRecord {
	rec := removeRecord{Kind: removeKindHopspace, Path: d.path, Removed: removed}
	if !removed {
		rec.Reason = d.reason()
	}
	return rec
}

// dataHomeHopspaceFor decides the fate of the data-home hopspace of
// hub's repository when the hub at hubPath is removed. st is the state
// (stErr the error reading it); hubPath's own entry is ignored.
func dataHomeHopspaceFor(fs afero.Fs, st *state.State, stErr error, hub *hop.Hub, hubPath string) dataHomeHopspace {
	path := hop.GetHopspacePath(hop.GetGitHopDataHome(), hop.RepoRefFor(hubPath, hub.Config.Repo))
	d := dataHomeHopspace{path: path, stateErr: stErr}
	d.ofHub = state.SamePath(hop.ResolveHopspacePath(hubPath, hub.Config.Repo), path)
	d.exists, _ = afero.DirExists(fs, path)
	if !d.exists || stErr != nil {
		return d
	}
	for _, repo := range st.Repositories {
		for _, h := range repo.Hubs {
			if h == nil || state.SamePath(h.Path, hubPath) {
				continue
			}
			if hubUsesHopspace(fs, repo, h, path) {
				d.usedBy = append(d.usedBy, h.Path)
			}
		}
	}
	sort.Strings(d.usedBy)
	return d
}

// hubUsesHopspace reports whether the recorded hub h uses the hopspace
// at path. Its own marker decides, as hop.ResolveHopspacePath does. A
// hub whose directory is gone uses nothing. One whose hop.json cannot be
// read falls back on the mode state recorded for it, so a hub that may
// still be repaired keeps its hopspace.
func hubUsesHopspace(fs afero.Fs, repo *state.RepositoryState, h *state.HubState, path string) bool {
	if exists, _ := afero.DirExists(fs, h.Path); !exists {
		return false
	}
	if other, err := hop.LoadHub(fs, h.Path); err == nil {
		return state.SamePath(hop.ResolveHopspacePath(h.Path, other.Config.Repo), path)
	}
	return h.Mode == state.HubModeGlobal &&
		state.SamePath(hop.GetHopspacePath(hop.GetGitHopDataHome(), hop.NewRepoRef(repo.URI, repo.Org, repo.Repo).In(h.Path)), path)
}
