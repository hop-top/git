package config

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/spf13/afero"
)

// Writer handles writing configuration files atomically
type Writer struct {
	fs afero.Fs
}

// NewWriter creates a new configuration writer
func NewWriter(fs afero.Fs) *Writer {
	return &Writer{fs: fs}
}

// WriteHubConfig writes the hub configuration
func (w *Writer) WriteHubConfig(path string, config *HubConfig) error {
	return w.writeConfig(filepath.Join(path, "hop.json"), config)
}

// WriteHopspaceConfig writes the hopspace configuration
func (w *Writer) WriteHopspaceConfig(path string, config *HopspaceConfig) error {
	return w.writeConfig(filepath.Join(path, "hop.json"), config)
}

// WritePortsConfig writes the ports configuration
func (w *Writer) WritePortsConfig(path string, config *PortsConfig) error {
	return w.writeConfig(filepath.Join(path, "ports.json"), config)
}

// WriteVolumesConfig writes the volumes configuration
func (w *Writer) WriteVolumesConfig(path string, config *VolumesConfig) error {
	return w.writeConfig(filepath.Join(path, "volumes.json"), config)
}

// writeConfig writes the config to a temp file and renames it (atomic)
func (w *Writer) writeConfig(path string, config interface{}) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	dir := filepath.Dir(path)
	if err := w.fs.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	tmpFile, err := afero.TempFile(w.fs, dir, "hop-config-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		if _, err := w.fs.Stat(tmpPath); err == nil {
			w.fs.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("failed to write to temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := w.fs.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to rename temp file to %s: %w", path, err)
	}

	return nil
}
