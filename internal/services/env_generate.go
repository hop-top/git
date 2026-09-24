package services

import (
	"path/filepath"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/docker"
	"hop.top/git/internal/output"
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
// worktreePath on branch: it allocates the branch's ports and volumes,
// writes the worktree's .env and, for a compose file with hardcoded host
// ports, the compose override, then records the allocation in the
// hopspace's ports.json and volumes.json. It is the one path add, clone
// and `env generate` share. A worktree without a Docker environment gets
// nothing: (nil, nil). A failure to record the allocation is reported
// and does not fail the call.
func GenerateWorktreeEnv(fs afero.Fs, d *docker.Docker, hopspacePath, worktreePath, branch, org, repo string) (*WorktreeEnv, error) {
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

	ports, vols, overridePath, err := NewEnvManager(fs, portsCfg, volsCfg, d).Generate(branch, worktreePath, org, repo)
	if err != nil {
		return nil, err
	}

	if portsCfg.Branches == nil {
		portsCfg.Branches = make(map[string]config.BranchPorts)
	}
	if volsCfg.Branches == nil {
		volsCfg.Branches = make(map[string]config.BranchVolumes)
	}
	portsCfg.Branches[branch] = *ports
	volsCfg.Branches[branch] = *vols
	writer := config.NewWriter(fs)
	if err := writer.WritePortsConfig(hopspacePath, portsCfg); err != nil {
		output.Error("Failed to save ports config: %v", err)
	}
	if err := writer.WriteVolumesConfig(hopspacePath, volsCfg); err != nil {
		output.Error("Failed to save volumes config: %v", err)
	}

	return &WorktreeEnv{Ports: ports, Volumes: vols, OverridePath: overridePath}, nil
}
