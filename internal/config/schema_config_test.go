package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test the new V2 configuration schema per the config-state-separation plan

func TestSchemaConfig_Defaults(t *testing.T) {
	cfg := NewSchemaConfig()

	// Check defaults
	assert.Equal(t, "github.com", cfg.Defaults.GitDomain)
	assert.True(t, cfg.Defaults.BareRepo)
	assert.False(t, cfg.Defaults.AutoEnvStart)
	assert.Equal(t, "${EDITOR}", cfg.Defaults.Editor)
	assert.Equal(t, "${SHELL}", cfg.Defaults.Shell)

	// Check output defaults
	assert.Equal(t, "human", cfg.Output.Format)
	assert.Equal(t, "auto", cfg.Output.ColorScheme)
	assert.False(t, cfg.Output.Verbose)
	assert.False(t, cfg.Output.Quiet)

	// Check ports defaults
	assert.Equal(t, "hash", cfg.Ports.AllocationMode)
	assert.Equal(t, 10000, cfg.Ports.BaseRange.Start)
	assert.Equal(t, 15000, cfg.Ports.BaseRange.End)

	// Check hooks are nil by default
	assert.Nil(t, cfg.Hooks.PreWorktreeAdd)
	assert.Nil(t, cfg.Hooks.PostWorktreeAdd)

	// Check doctor defaults
	assert.False(t, cfg.Doctor.AutoFix)
	assert.Contains(t, cfg.Doctor.ChecksEnabled, "worktreeState")
}

func TestLoadSchemaConfig_NewFile(t *testing.T) {
	fs := afero.NewMemMapFs()

	cfg, err := LoadSchemaConfig(fs)

	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, "github.com", cfg.Defaults.GitDomain)
}

func TestLoadSchemaConfig_ExistingFile(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create config file matching new schema
	hookPath := "/path/to/hook.sh"
	configData := SchemaConfig{
		Defaults: DefaultsSchema{
			GitDomain:    "gitlab.com",
			BareRepo:     false,
			AutoEnvStart: true,
			Editor:       "vim",
			Shell:        "/bin/zsh",
		},
		Output: OutputSchema{
			Format:      "json",
			ColorScheme: "always",
			Verbose:     true,
			Quiet:       false,
		},
		Ports: PortsSchema{
			AllocationMode: "sequential",
			BaseRange: PortRangeSchema{
				Start: 20000,
				End:   25000,
			},
		},
		Volumes: VolumesSchema{
			BasePath: "/custom/volumes",
			Cleanup: VolumeCleanupSchema{
				OnRemove:          true,
				OrphanedAfterDays: 60,
			},
		},
		Hooks: HooksSchema{
			PreWorktreeAdd:  &hookPath,
			PostWorktreeAdd: nil,
			PreEnvStart:     nil,
			PostEnvStart:    nil,
			PreEnvStop:      nil,
			PostEnvStop:     nil,
		},
		Doctor: DoctorSchema{
			AutoFix:       true,
			ChecksEnabled: []string{"worktreeState"},
		},
	}

	configPath := filepath.Join(GetConfigHome(), "config.json")
	require.NoError(t, fs.MkdirAll(filepath.Dir(configPath), 0755))

	data, err := json.MarshalIndent(configData, "", "  ")
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, configPath, data, 0644))

	// Load the config
	cfg, err := LoadSchemaConfig(fs)

	require.NoError(t, err)
	assert.Equal(t, "gitlab.com", cfg.Defaults.GitDomain)
	assert.Equal(t, "json", cfg.Output.Format)
	assert.Equal(t, 20000, cfg.Ports.BaseRange.Start)
	assert.True(t, cfg.Doctor.AutoFix)
	assert.NotNil(t, cfg.Hooks.PreWorktreeAdd)
	assert.Equal(t, "/path/to/hook.sh", *cfg.Hooks.PreWorktreeAdd)
}

func TestSaveSchemaConfig(t *testing.T) {
	fs := afero.NewMemMapFs()

	cfg := NewSchemaConfig()
	cfg.Defaults.GitDomain = "custom.git"

	err := SaveSchemaConfig(fs, cfg)

	require.NoError(t, err)

	// Verify file was created
	configPath := filepath.Join(GetConfigHome(), "config.json")
	exists, err := afero.Exists(fs, configPath)
	require.NoError(t, err)
	assert.True(t, exists)

	// Verify content
	data, err := afero.ReadFile(fs, configPath)
	require.NoError(t, err)

	var loaded SchemaConfig
	require.NoError(t, json.Unmarshal(data, &loaded))
	assert.Equal(t, "custom.git", loaded.Defaults.GitDomain)
}

func TestSchemaConfig_ExpandVariables(t *testing.T) {
	// Set environment variables for testing
	os.Setenv("TEST_EDITOR", "nano")
	os.Setenv("TEST_SHELL", "/bin/fish")
	defer os.Unsetenv("TEST_EDITOR")
	defer os.Unsetenv("TEST_SHELL")

	cfg := &SchemaConfig{
		Defaults: DefaultsSchema{
			Editor: "${TEST_EDITOR}",
			Shell:  "${TEST_SHELL}",
		},
	}

	assert.Equal(t, "nano", cfg.GetEditor())
	assert.Equal(t, "/bin/fish", cfg.GetShell())
}

func TestSchemaConfig_IsCheckEnabled(t *testing.T) {
	cfg := NewSchemaConfig()

	assert.True(t, cfg.IsCheckEnabled("worktreeState"))
	assert.True(t, cfg.IsCheckEnabled("configConsistency"))
	assert.False(t, cfg.IsCheckEnabled("nonExistentCheck"))
}
