package services

import (
	"fmt"
	"path/filepath"

	"github.com/jadb/git-hop/internal/config"
	"github.com/spf13/afero"
)

// VolumeManager handles volume creation
type VolumeManager struct {
	Config *config.VolumesConfig
	fs     afero.Fs
}

// NewVolumeManager creates a new manager
func NewVolumeManager(fs afero.Fs, cfg *config.VolumesConfig) *VolumeManager {
	return &VolumeManager{Config: cfg, fs: fs}
}

// CreateVolumes creates volume directories for a branch
func (m *VolumeManager) CreateVolumes(branch string, volumeNames []string) (map[string]string, error) {
	volumes := make(map[string]string)
	for _, name := range volumeNames {
		volName := fmt.Sprintf("hop_%s_%s", branch, name)
		volPath := filepath.Join(m.Config.BasePath, volName)

		if err := m.fs.MkdirAll(volPath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create volume dir %s: %w", volPath, err)
		}
		volumes[name] = volPath
	}
	return volumes, nil
}
