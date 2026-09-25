package services

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
)

// VolumeManager handles volume creation
type VolumeManager struct {
	Config *config.VolumesConfig
	// Keep holds the directories, by volume name, the worktree's entry
	// already records; they are used as they are, never moved.
	Keep map[string]string
	// Dir is where new volume directories go; empty means Config.BasePath.
	Dir string
	fs  afero.Fs
}

// NewVolumeManager creates a new manager
func NewVolumeManager(fs afero.Fs, cfg *config.VolumesConfig) *VolumeManager {
	return &VolumeManager{Config: cfg, fs: fs}
}

// CreateVolumes returns a directory for every volume name of branch,
// creating it when missing: the one Keep records, else
// <Dir>/hop_<branch>_<name>.
func (m *VolumeManager) CreateVolumes(branch string, volumeNames []string) (map[string]string, error) {
	dir := m.Dir
	if dir == "" {
		dir = m.Config.BasePath
	}
	volumes := make(map[string]string)
	for _, name := range volumeNames {
		volPath, ok := m.Keep[name]
		if !ok {
			volPath = filepath.Join(dir, fmt.Sprintf("hop_%s_%s", branch, name))
		}
		if err := m.fs.MkdirAll(volPath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create volume dir %s: %w", volPath, err)
		}
		volumes[name] = volPath
	}
	return volumes, nil
}
