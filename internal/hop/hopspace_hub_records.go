package hop

import (
	"maps"

	"hop.top/git/internal/config"
	"hop.top/git/internal/state"
)

// HubRecords returns the keys of the records a hopspace several --global
// hubs share holds for the worktrees of the hub at hubPath, as an Update
// for that hub sees them: entries an earlier release keyed by branch are
// migrated first (migrateSharedEntries). A hub's own hopspace has none to
// report: it goes with the hub.
func (h *Hopspace) HubRecords(hubPath string) []string {
	if h == nil || h.Config == nil || !sharedHopspace(h.Path, hubPath) {
		return nil
	}
	cfg := &config.HopspaceConfig{Branches: maps.Clone(h.Config.Branches)}
	migrateSharedEntries(cfg, worktreeOwner(h.fs, hubPath))
	var keys []string
	for _, k := range sortedEntryKeys(cfg.Branches) {
		if !isBranchKey(k) && recordOfHub(cfg.Branches[k], hubPath) {
			keys = append(keys, k)
		}
	}
	return keys
}

// DropHubRecords removes the records of the worktrees of the hub at
// hubPath from a hopspace several --global hubs share, for a hub removed
// while other hubs keep the hopspace. It returns the keys removed. A
// hub's own hopspace is left alone.
func (h *Hopspace) DropHubRecords(hubPath string) ([]string, error) {
	if !sharedHopspace(h.Path, hubPath) {
		return nil, nil
	}
	return h.DropRecords(hubPath, func(_ string, e config.HopspaceBranch) bool {
		return recordOfHub(e, hubPath)
	})
}

// DropRecords removes the worktree records (the entries keyed by a
// worktree's path) drop selects, deciding on hop.json as it is on disk
// under its lock (Update, for the hub at hubPath). Branch-keyed entries
// are never offered. It returns the keys removed; nothing is written when
// there are none.
func (h *Hopspace) DropRecords(hubPath string, drop func(key string, e config.HopspaceBranch) bool) ([]string, error) {
	var keys []string
	err := h.Update(hubPath, func(cfg *config.HopspaceConfig) error {
		keys = nil
		for _, k := range sortedEntryKeys(cfg.Branches) {
			if isBranchKey(k) || !drop(k, cfg.Branches[k]) {
				continue
			}
			delete(cfg.Branches, k)
			keys = append(keys, k)
		}
		if len(keys) == 0 {
			return errUnchanged
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

// recordOfHub reports whether e records a worktree of the hub at hubPath.
func recordOfHub(e config.HopspaceBranch, hubPath string) bool {
	return e.Hub != "" && state.SamePath(e.Hub, hubPath)
}

// Records returns the keys of the worktree records (the entries keyed by
// a worktree's path) of the hopspace, as loaded, that match selects.
func (h *Hopspace) Records(match func(key string, e config.HopspaceBranch) bool) []string {
	if h == nil || h.Config == nil {
		return nil
	}
	var keys []string
	for _, k := range sortedEntryKeys(h.Config.Branches) {
		if !isBranchKey(k) && match(k, h.Config.Branches[k]) {
			keys = append(keys, k)
		}
	}
	return keys
}
