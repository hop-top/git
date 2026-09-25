package services

import (
	"path/filepath"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/docker"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// WorktreeEnv is the environment GenerateWorktreeEnv prepared for a
// worktree.
type WorktreeEnv struct {
	Ports   *config.BranchPorts
	Volumes *config.BranchVolumes
	// OverridePath is the compose override written for the branch, ""
	// when the compose file needs none.
	OverridePath string
}

// GenerateWorktreeEnv prepares the Docker environment of the worktree at
// worktreePath on branch, in the hub at hubPath: it allocates the
// branch's ports and volumes, writes the worktree's .env and, for a
// compose file with hardcoded host ports, the compose override (in the
// hub's own cache directory, HubOverrideDir), then records the
// allocation in the hopspace's ports.json and volumes.json. It is the one path add, clone
// and `env generate` share. A worktree without a Docker environment gets
// nothing: (nil, nil). A failure to record the allocation is reported
// and does not fail the call.
func GenerateWorktreeEnv(fs afero.Fs, d *docker.Docker, hopspacePath, hubPath, worktreePath, branch, org, repo string) (*WorktreeEnv, error) {
	if !d.HasDockerEnv(worktreePath) {
		return nil, nil
	}

	loader := config.NewLoader(fs)
	portsCfg, err := loader.LoadPortsConfig(hopspacePath)
	if err != nil {
		portsCfg = &config.PortsConfig{
			AllocationMode: "incremental",
			BaseRange:      config.PortRange{Start: 10000, End: 20000},
			Branches:       make(map[string]config.BranchPorts),
		}
	}
	volsCfg, err := loader.LoadVolumesConfig(hopspacePath)
	if err != nil {
		volsCfg = &config.VolumesConfig{
			BasePath: filepath.Join(hopspacePath, "volumes"),
			Branches: make(map[string]config.BranchVolumes),
		}
	}

	recs, err := LoadEnvRecords(fs, hubPath)
	if err != nil {
		output.Warn("cannot read the port allocations of other hubs: %v", err)
	}
	self, found := recs.Self(hopspacePath, hubPath, worktreePath, branch)
	keep := keptPorts(recs, self, found)

	manager := NewEnvManager(fs, portsCfg, volsCfg, d)
	manager.Ports.Keep = keep
	manager.Ports.Reserved = recs.Reserved(self)
	if hubPath != "" {
		manager.OverrideDir = HubOverrideDir(org, repo, hubPath, branch)
		manager.Ports.Seed = org + "/" + repo + "/" + HubKey(hubPath) + "/" + branch
	}
	ports, vols, overridePath, err := manager.Generate(branch, worktreePath, org, repo)
	if err != nil {
		return nil, err
	}
	if overridePath != "" {
		ports.OverrideDir = filepath.Dir(overridePath)
	}
	ports.Project = projectFor(self, found && keep != nil, org, repo, hubPath, branch)
	ports.Branch = branch
	ports.Worktree = state.WorktreeKey(worktreePath)
	ports.Hub = hubPath

	if portsCfg.Branches == nil {
		portsCfg.Branches = make(map[string]config.BranchPorts)
	}
	if volsCfg.Branches == nil {
		volsCfg.Branches = make(map[string]config.BranchVolumes)
	}
	key := EnvRecordKey(hopspacePath, hubPath, worktreePath, branch)
	if found && self.Key != key {
		// The branch-keyed entry an earlier release wrote in a shared
		// hopspace moves under this worktree's key.
		delete(portsCfg.Branches, self.Key)
		delete(volsCfg.Branches, self.Key)
	}
	portsCfg.Branches[key] = *ports
	volsCfg.Branches[key] = *vols
	writer := config.NewWriter(fs)
	if err := writer.WritePortsConfig(hopspacePath, portsCfg); err != nil {
		output.Error("Failed to save ports config: %v", err)
	}
	if err := writer.WriteVolumesConfig(hopspacePath, volsCfg); err != nil {
		output.Error("Failed to save volumes config: %v", err)
	}

	return &WorktreeEnv{Ports: ports, Volumes: vols, OverridePath: overridePath}, nil
}

// keptPorts returns the ports the worktree keeps: all it has, unless a
// claim ordered before it (the hub set up first) holds one of them, in
// which case it gets new ones and each conflict is reported. nil means
// nothing is kept.
func keptPorts(recs *EnvRecords, self EnvClaim, found bool) map[string]int {
	if !found || len(self.Entry.Ports) == 0 {
		return nil
	}
	conflicts := recs.Conflicts(self)
	if len(conflicts) == 0 {
		return self.Entry.Ports
	}
	for _, c := range conflicts {
		output.Warn("port %d (%s) is also allocated to %s of %s, set up first; allocating new ports",
			c.Port, c.Service, c.Other.Branch, c.Other.Hub)
	}
	return nil
}

// projectFor returns the compose project the worktree runs as. One that
// keeps its ports keeps its project: the one its entry records, or
// <org>-<repo>-<branch> for an entry an earlier release wrote, so its
// running containers stay reachable. One with new ports runs as its
// hub's own (HubComposeProjectName).
func projectFor(self EnvClaim, kept bool, org, repo, hubPath, branch string) string {
	if kept {
		if self.Entry.Project != "" {
			return self.Entry.Project
		}
		return ComposeProjectName(org, repo, branch)
	}
	if hubPath == "" {
		return ComposeProjectName(org, repo, branch)
	}
	return HubComposeProjectName(org, repo, hubPath, branch)
}
