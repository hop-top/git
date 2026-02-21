package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hop.top/git/internal/config"
	"hop.top/git/internal/docker"
	"hop.top/git/internal/hop"
	"github.com/spf13/afero"
)

// EnvManager manages the environment
type EnvManager struct {
	Ports   *PortAllocator
	Volumes *VolumeManager
	Docker  *docker.Docker
	fs      afero.Fs
}

// NewEnvManager creates a new env manager
func NewEnvManager(fs afero.Fs, portsCfg *config.PortsConfig, volsCfg *config.VolumesConfig, d *docker.Docker) *EnvManager {
	return &EnvManager{
		Ports:   NewPortAllocator(portsCfg),
		Volumes: NewVolumeManager(fs, volsCfg),
		Docker:  d,
		fs:      fs,
	}
}

// overrideMeta stores hash for cache invalidation
type overrideMeta struct {
	ComposeHash string `json:"composeHash"`
}

// Generate ensures ports and volumes exist for a branch.
// When org and repo are provided, it also generates a docker-compose override file
// for services with hardcoded ports. Returns the override path (empty if not needed).
func (m *EnvManager) Generate(branch, worktreePath, org, repo string) (*config.BranchPorts, *config.BranchVolumes, string, error) {
	var overridePath string

	// Try to read raw compose file content to detect hardcoded ports
	composeFileName := docker.FindComposeFile(worktreePath)
	var composeContent []byte
	if composeFileName != "" {
		var err error
		composeContent, err = os.ReadFile(filepath.Join(worktreePath, composeFileName))
		if err != nil {
			composeContent = nil
		}
	}

	// Determine service names for port allocation
	var portVarNames []string
	if len(composeContent) > 0 {
		servicePorts, err := docker.ParsePortMappings(composeContent)
		if err == nil && docker.NeedsOverride(servicePorts) && org != "" && repo != "" {
			// Generate override file
			overrideYAML, _ := docker.GenerateOverride(servicePorts)
			portVarNames = docker.ComputePortVarNames(servicePorts)

			overridePath = hop.GetComposeOverrideCachePath(org, repo, branch)

			// Check cache: skip regeneration if compose file hash matches
			if !m.needsRegeneration(composeContent, org, repo, branch) {
				// Override already up to date, just use existing port var names
			} else {
				// Write override file
				overrideDir := filepath.Dir(overridePath)
				if err := os.MkdirAll(overrideDir, 0755); err != nil {
					return nil, nil, "", fmt.Errorf("failed to create override cache dir: %w", err)
				}
				if err := os.WriteFile(overridePath, overrideYAML, 0644); err != nil {
					return nil, nil, "", fmt.Errorf("failed to write override file: %w", err)
				}

				// Write meta file for cache invalidation
				m.writeOverrideMeta(composeContent, org, repo, branch)
			}
		}
	}

	// If we computed port var names from override, use those as service names
	// Otherwise fall back to docker compose service names
	if len(portVarNames) > 0 {
		existingSvcs := make(map[string]bool)
		for _, s := range m.Ports.Config.Services {
			existingSvcs[s] = true
		}
		for _, name := range portVarNames {
			if !existingSvcs[name] {
				m.Ports.Config.Services = append(m.Ports.Config.Services, name)
			}
		}
	} else {
		serviceNames, err := m.Docker.GetServiceNames(worktreePath)
		if err != nil {
			return nil, nil, "", err
		}

		existingSvcs := make(map[string]bool)
		for _, s := range m.Ports.Config.Services {
			existingSvcs[s] = true
		}
		for _, s := range serviceNames {
			if !existingSvcs[s] {
				m.Ports.Config.Services = append(m.Ports.Config.Services, s)
			}
		}
	}

	ports, err := m.Ports.AllocatePorts(branch)
	if err != nil {
		return nil, nil, "", err
	}

	volNames, err := m.Docker.GetVolumeNames(worktreePath)
	if err != nil {
		return nil, nil, "", err
	}

	vols, err := m.Volumes.CreateVolumes(branch, volNames)
	if err != nil {
		return nil, nil, "", err
	}

	envPath := filepath.Join(worktreePath, ".env")
	if err := m.writeEnvFile(envPath, ports, vols); err != nil {
		return nil, nil, "", fmt.Errorf("failed to write .env file: %w", err)
	}

	return &config.BranchPorts{Ports: ports}, &config.BranchVolumes{Volumes: vols}, overridePath, nil
}

func (m *EnvManager) needsRegeneration(composeContent []byte, org, repo, branch string) bool {
	metaPath := hop.GetOverrideMetaCachePath(org, repo, branch)
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		return true
	}

	var meta overrideMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return true
	}

	hash := sha256.Sum256(composeContent)
	return meta.ComposeHash != hex.EncodeToString(hash[:])
}

func (m *EnvManager) writeOverrideMeta(composeContent []byte, org, repo, branch string) {
	hash := sha256.Sum256(composeContent)
	meta := overrideMeta{ComposeHash: hex.EncodeToString(hash[:])}
	data, err := json.Marshal(meta)
	if err != nil {
		return
	}
	metaPath := hop.GetOverrideMetaCachePath(org, repo, branch)
	os.WriteFile(metaPath, data, 0644)
}

func (m *EnvManager) writeEnvFile(path string, ports map[string]int, vols map[string]string) error {
	var lines []string
	lines = append(lines, "# Generated by git-hop")

	for svc, port := range ports {
		key := fmt.Sprintf("HOP_PORT_%s", strings.ToUpper(svc))
		lines = append(lines, fmt.Sprintf("%s=%d", key, port))
	}

	for vol, path := range vols {
		key := fmt.Sprintf("HOP_VOLUME_%s", strings.ToUpper(vol))
		lines = append(lines, fmt.Sprintf("%s=%s", key, path))
	}

	return afero.WriteFile(m.fs, path, []byte(strings.Join(lines, "\n")), 0644)
}
