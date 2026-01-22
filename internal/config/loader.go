package config

import (
	"encoding/json"
	"path/filepath"

	"github.com/spf13/afero"
	"github.com/spf13/viper"
)

// Loader handles loading configuration from various sources
type Loader struct {
	fs afero.Fs
}

// NewLoader creates a new configuration loader
func NewLoader(fs afero.Fs) *Loader {
	return &Loader{fs: fs}
}

// LoadHubConfig loads the hub configuration from the given path
func (l *Loader) LoadHubConfig(path string) (*HubConfig, error) {
	configPath := filepath.Join(path, "hop.json")
	return loadConfig[HubConfig](l.fs, configPath)
}

// LoadHopspaceConfig loads the hopspace configuration
func (l *Loader) LoadHopspaceConfig(path string) (*HopspaceConfig, error) {
	configPath := filepath.Join(path, "hop.json")
	return loadConfig[HopspaceConfig](l.fs, configPath)
}

// LoadPortsConfig loads the ports configuration
func (l *Loader) LoadPortsConfig(path string) (*PortsConfig, error) {
	configPath := filepath.Join(path, "ports.json")
	return loadConfig[PortsConfig](l.fs, configPath)
}

// LoadVolumesConfig loads the volumes configuration
func (l *Loader) LoadVolumesConfig(path string) (*VolumesConfig, error) {
	configPath := filepath.Join(path, "volumes.json")
	return loadConfig[VolumesConfig](l.fs, configPath)
}

// Helper generic function to load config
func loadConfig[T any](fs afero.Fs, path string) (*T, error) {
	data, err := afero.ReadFile(fs, path)
	if err != nil {
		return nil, err
	}

	var config T
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// GetGlobalString returns a string from global config (viper)
func GetGlobalString(key string) string {
	return viper.GetString(key)
}

// GetGlobalStringSlice returns a string slice from global config (viper)
func GetGlobalStringSlice(key string) []string {
	return viper.GetStringSlice(key)
}
