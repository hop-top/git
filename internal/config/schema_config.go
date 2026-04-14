package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
	"hop.top/kit/xdg"
)

// SchemaConfig represents the new git-hop configuration schema per config-state-separation plan
type SchemaConfig struct {
	Defaults DefaultsSchema `json:"defaults"`
	Output   OutputSchema   `json:"output"`
	Ports    PortsSchema    `json:"ports"`
	Volumes  VolumesSchema  `json:"volumes"`
	Hooks    HooksSchema    `json:"hooks"`
	Doctor   DoctorSchema   `json:"doctor"`
}

// DefaultsSchema represents default settings
type DefaultsSchema struct {
	GitDomain    string `json:"gitDomain"`
	BareRepo     bool   `json:"bareRepo"`
	AutoEnvStart bool   `json:"autoEnvStart"`
	Editor       string `json:"editor"`
	Shell        string `json:"shell"`
}

// OutputSchema represents output settings
type OutputSchema struct {
	Format      string `json:"format"`      // "human", "json", "porcelain"
	ColorScheme string `json:"colorScheme"` // "auto", "always", "never"
	Verbose     bool   `json:"verbose"`
	Quiet       bool   `json:"quiet"`
}

// PortsSchema represents port allocation settings
type PortsSchema struct {
	AllocationMode string          `json:"allocationMode"` // "hash", "sequential", "random"
	BaseRange      PortRangeSchema `json:"baseRange"`
}

// PortRangeSchema represents a port range
type PortRangeSchema struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// VolumesSchema represents volume settings
type VolumesSchema struct {
	BasePath string              `json:"basePath"`
	Cleanup  VolumeCleanupSchema `json:"cleanup"`
}

// VolumeCleanupSchema represents volume cleanup settings
type VolumeCleanupSchema struct {
	OnRemove          bool `json:"onRemove"`
	OrphanedAfterDays int  `json:"orphanedAfterDays"`
}

// HooksSchema represents hook paths
type HooksSchema struct {
	PreWorktreeAdd  *string `json:"preWorktreeAdd"`
	PostWorktreeAdd *string `json:"postWorktreeAdd"`
	PreEnvStart     *string `json:"preEnvStart"`
	PostEnvStart    *string `json:"postEnvStart"`
	PreEnvStop      *string `json:"preEnvStop"`
	PostEnvStop     *string `json:"postEnvStop"`
}

// DoctorSchema represents doctor command settings
type DoctorSchema struct {
	AutoFix       bool     `json:"autoFix"`
	ChecksEnabled []string `json:"checksEnabled"`
}

func getDataHome() string {
	dir, err := xdg.DataDir("git-hop")
	if err != nil {
		return filepath.Join(".local", "share")
	}
	return filepath.Dir(dir)
}

// NewSchemaConfig returns a new V2 config with default values
func NewSchemaConfig() *SchemaConfig {
	dataHome := getDataHome()

	return &SchemaConfig{
		Defaults: DefaultsSchema{
			GitDomain:    "github.com",
			BareRepo:     true,
			AutoEnvStart: false,
			Editor:       "${EDITOR}",
			Shell:        "${SHELL}",
		},
		Output: OutputSchema{
			Format:      "human",
			ColorScheme: "auto",
			Verbose:     false,
			Quiet:       false,
		},
		Ports: PortsSchema{
			AllocationMode: "hash",
			BaseRange: PortRangeSchema{
				Start: 10000,
				End:   15000,
			},
		},
		Volumes: VolumesSchema{
			BasePath: filepath.Join(dataHome, "git-hop", "volumes"),
			Cleanup: VolumeCleanupSchema{
				OnRemove:          true,
				OrphanedAfterDays: 30,
			},
		},
		Hooks: HooksSchema{
			PreWorktreeAdd:  nil,
			PostWorktreeAdd: nil,
			PreEnvStart:     nil,
			PostEnvStart:    nil,
			PreEnvStop:      nil,
			PostEnvStop:     nil,
		},
		Doctor: DoctorSchema{
			AutoFix: false,
			ChecksEnabled: []string{
				"worktreeState",
				"configConsistency",
				"orphanedDirectories",
				"gitMetadata",
			},
		},
	}
}

func GetConfigHome() string {
	dir, err := xdg.ConfigDir("git-hop")
	if err != nil {
		return filepath.Join(".config", "git-hop")
	}
	return dir
}

// LoadSchemaConfig loads the V2 config from disk or returns default config
func LoadSchemaConfig(fs afero.Fs) (*SchemaConfig, error) {
	configPath := filepath.Join(GetConfigHome(), "config.json")

	exists, err := afero.Exists(fs, configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to check config file: %w", err)
	}

	if !exists {
		return NewSchemaConfig(), nil
	}

	data, err := afero.ReadFile(fs, configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg SchemaConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &cfg, nil
}

// SaveSchemaConfig saves the V2 config to disk
func SaveSchemaConfig(fs afero.Fs, cfg *SchemaConfig) error {
	configDir := GetConfigHome()
	configPath := filepath.Join(configDir, "config.json")

	if err := fs.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := afero.WriteFile(fs, configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// ExpandVariables expands environment variables in a string using ${VAR} syntax
func ExpandVariables(s string) string {
	return os.Expand(s, func(key string) string {
		return os.Getenv(key)
	})
}

// GetEditor returns the configured editor with variables expanded
func (c *SchemaConfig) GetEditor() string {
	return ExpandVariables(c.Defaults.Editor)
}

// GetShell returns the configured shell with variables expanded
func (c *SchemaConfig) GetShell() string {
	return ExpandVariables(c.Defaults.Shell)
}

// GetVolumesBasePath returns the volumes base path with variables expanded
func (c *SchemaConfig) GetVolumesBasePath() string {
	return ExpandVariables(c.Volumes.BasePath)
}

// IsCheckEnabled returns whether a doctor check is enabled
func (c *SchemaConfig) IsCheckEnabled(checkName string) bool {
	for _, check := range c.Doctor.ChecksEnabled {
		if check == checkName {
			return true
		}
	}
	return false
}
