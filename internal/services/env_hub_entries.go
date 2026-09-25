package services

import (
	"path/filepath"
	"sort"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/state"
)

// EnvEntry is a worktree's entry in a hopspace's ports.json, and whether
// its volumes.json records volumes under the same key.
type EnvEntry struct {
	Key string
	// Branch and Worktree are the entry's, or taken from its key: a
	// branch in a hub's own hopspace, a worktree path in a shared one.
	Branch, Worktree string
	// Volumes reports whether volumes.json has an entry under Key.
	Volumes bool
}

// HubEnvEntries returns the entries of the hopspace at hopspacePath
// recorded for the hub at hubPath, by key: the ones DropHubEnvEntries
// removes. Nothing is written.
func HubEnvEntries(fs afero.Fs, hopspacePath, hubPath string) []EnvEntry {
	loader := config.NewLoader(fs)
	ports, err := loader.LoadPortsConfig(hopspacePath)
	if err != nil {
		return nil
	}
	vols, _ := loader.LoadVolumesConfig(hopspacePath)
	return envEntries(ports, vols, ofHub(hubPath))
}

// DropHubEnvEntries removes from hopspacePath the entries recorded for
// the hub at hubPath, for a hub gone from a hopspace other hubs keep, and
// returns them. Only the records go: the volume directories they name,
// and the data in them, are left alone.
func DropHubEnvEntries(fs afero.Fs, hopspacePath, hubPath string) ([]EnvEntry, error) {
	return dropEnvEntries(fs, hopspacePath, ofHub(hubPath))
}

// ofHub selects the entries naming the hub at hubPath as theirs. An
// entry an earlier release wrote names no hub and is never selected.
func ofHub(hubPath string) func(string, config.BranchPorts) bool {
	return func(_ string, e config.BranchPorts) bool {
		return e.Hub != "" && state.SamePath(e.Hub, hubPath)
	}
}

// dropEnvEntries removes the ports.json entries drop selects, and the
// volumes.json entries under the same keys, and returns them. A file is
// written only when an entry of it goes.
func dropEnvEntries(fs afero.Fs, hopspacePath string, drop func(string, config.BranchPorts) bool) ([]EnvEntry, error) {
	loader, writer := config.NewLoader(fs), config.NewWriter(fs)
	ports, err := loader.LoadPortsConfig(hopspacePath)
	if err != nil {
		return nil, nil
	}
	vols, verr := loader.LoadVolumesConfig(hopspacePath)
	if verr != nil {
		vols = nil
	}
	entries := envEntries(ports, vols, drop)
	if len(entries) == 0 {
		return nil, nil
	}
	volumes := false
	for _, e := range entries {
		delete(ports.Branches, e.Key)
		if e.Volumes {
			delete(vols.Branches, e.Key)
			volumes = true
		}
	}
	if err := writer.WritePortsConfig(hopspacePath, ports); err != nil {
		return nil, err
	}
	if volumes {
		if err := writer.WriteVolumesConfig(hopspacePath, vols); err != nil {
			// ports.json lost them already.
			for i := range entries {
				entries[i].Volumes = false
			}
			return entries, err
		}
	}
	return entries, nil
}

// envEntries returns the entries of ports that match selects, by key,
// noting which have volumes in vols (nil when there is none).
func envEntries(ports *config.PortsConfig, vols *config.VolumesConfig, match func(string, config.BranchPorts) bool) []EnvEntry {
	var out []EnvEntry
	for k, e := range ports.Branches {
		if !match(k, e) {
			continue
		}
		entry := EnvEntry{Key: k, Branch: e.Branch, Worktree: e.Worktree}
		if entry.Branch == "" && !filepath.IsAbs(k) {
			entry.Branch = k
		}
		if entry.Worktree == "" && filepath.IsAbs(k) {
			entry.Worktree = k
		}
		if vols != nil {
			_, entry.Volumes = vols.Branches[k]
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
