package config_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"hop.top/git/internal/config"
)

// fakeGitConfig returns a GitConfig backed by an in-memory map,
// avoiding real git calls during tests.
func fakeGitConfig(store map[string]string) *config.GitConfig {
	return &config.GitConfig{
		RunCmd: func(args ...string) (string, error) {
			// Handle "config --get <key>"
			if len(args) == 3 && args[0] == "config" && args[1] == "--get" {
				if v, ok := store[args[2]]; ok {
					return v, nil
				}
				return "", fmt.Errorf("key not found")
			}
			// Handle "config --global <key> <value>"
			if len(args) == 4 && args[0] == "config" && args[1] == "--global" {
				store[args[2]] = args[3]
				return "", nil
			}
			return "", fmt.Errorf("unexpected args: %v", args)
		},
	}
}

func TestLoad_DefaultsFromGitConfig(t *testing.T) {
	store := map[string]string{}
	gc := fakeGitConfig(store)
	loader := config.NewGlobalLoaderWithGitConfig(gc)

	cfg := loader.Load()

	// gitconfig.go defaults: gitDomain=github.com, env.autoStart=false
	if cfg.Defaults.EnvAutoStart != false {
		t.Errorf("EnvAutoStart = %v, want false", cfg.Defaults.EnvAutoStart)
	}
	if cfg.Defaults.GitDomain != "github.com" {
		t.Errorf("GitDomain = %q, want %q", cfg.Defaults.GitDomain, "github.com")
	}
	if cfg.ShellIntegration.Status != "unknown" {
		t.Errorf("ShellIntegration.Status = %q, want %q",
			cfg.ShellIntegration.Status, "unknown")
	}
	if cfg.Backup.MaxBackups != 3 {
		t.Errorf("Backup.MaxBackups = %d, want 3", cfg.Backup.MaxBackups)
	}
}

func TestLoad_OverridesFromGitConfig(t *testing.T) {
	store := map[string]string{
		"hop.gitDomain":               "gitlab.com",
		"hop.env.autoStart":           "true",
		"hop.worktreeLocation":        "/custom/{branch}",
		"hop.backup.maxBackups":       "10",
		"hop.shellIntegration.status": "approved",
		"hop.shellIntegration.shell":  "zsh",
	}
	gc := fakeGitConfig(store)
	loader := config.NewGlobalLoaderWithGitConfig(gc)

	cfg := loader.Load()

	if cfg.Defaults.GitDomain != "gitlab.com" {
		t.Errorf("GitDomain = %q, want %q", cfg.Defaults.GitDomain, "gitlab.com")
	}
	if cfg.Defaults.EnvAutoStart != true {
		t.Errorf("EnvAutoStart = %v, want true", cfg.Defaults.EnvAutoStart)
	}
	if cfg.Defaults.WorktreeLocation != "/custom/{branch}" {
		t.Errorf("WorktreeLocation = %q, want %q",
			cfg.Defaults.WorktreeLocation, "/custom/{branch}")
	}
	if cfg.Backup.MaxBackups != 10 {
		t.Errorf("Backup.MaxBackups = %d, want 10", cfg.Backup.MaxBackups)
	}
	if cfg.ShellIntegration.Status != "approved" {
		t.Errorf("ShellIntegration.Status = %q, want %q",
			cfg.ShellIntegration.Status, "approved")
	}
	if cfg.ShellIntegration.InstalledShell != "zsh" {
		t.Errorf("ShellIntegration.InstalledShell = %q, want %q",
			cfg.ShellIntegration.InstalledShell, "zsh")
	}
}

func TestMigration_JSONToGitConfig(t *testing.T) {
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))
	defer os.Unsetenv("XDG_CONFIG_HOME")

	// Write a legacy global.json
	configDir := filepath.Join(tmpDir, ".config", "git-hop")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}

	legacy := config.GlobalConfig{
		Defaults: config.DefaultSettings{
			EnvAutoStart: false,
			GitDomain:    "gitlab.com",
		},
		ShellIntegration: config.ShellIntegrationSettings{
			Status: "approved",
		},
		Backup: config.BackupSettings{
			MaxBackups: 7,
		},
		PackageManagers: []config.PackageManagerConfig{
			{Name: "custom-pm", DetectFiles: []string{"custom.lock"}},
		},
	}

	data, _ := json.MarshalIndent(legacy, "", "  ")
	jsonPath := filepath.Join(configDir, "global.json")
	if err := os.WriteFile(jsonPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	// Create loader with empty git config store
	store := map[string]string{}
	gc := fakeGitConfig(store)
	loader := config.NewGlobalLoaderWithGitConfig(gc)

	cfg := loader.Load()

	// Verify scalars migrated to git config
	if cfg.Defaults.GitDomain != "gitlab.com" {
		t.Errorf("migrated GitDomain = %q, want %q",
			cfg.Defaults.GitDomain, "gitlab.com")
	}
	if cfg.ShellIntegration.Status != "approved" {
		t.Errorf("migrated ShellIntegration.Status = %q, want %q",
			cfg.ShellIntegration.Status, "approved")
	}

	// Verify managers extracted to sidecar
	managersPath := filepath.Join(configDir, "managers.json")
	if _, err := os.Stat(managersPath); os.IsNotExist(err) {
		t.Error("managers.json was not created during migration")
	}

	if len(cfg.PackageManagers) != 1 || cfg.PackageManagers[0].Name != "custom-pm" {
		t.Errorf("migrated PackageManagers = %v, want 1 entry named custom-pm",
			cfg.PackageManagers)
	}

	// Verify legacy JSON renamed to .bak
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Error("global.json should have been renamed to .bak")
	}
	bakPath := jsonPath + ".bak"
	if _, err := os.Stat(bakPath); os.IsNotExist(err) {
		t.Error("global.json.bak should exist after migration")
	}
}

func TestMigration_SkipsWhenAlreadyMigrated(t *testing.T) {
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))
	defer os.Unsetenv("XDG_CONFIG_HOME")

	// Write a legacy global.json
	configDir := filepath.Join(tmpDir, ".config", "git-hop")
	os.MkdirAll(configDir, 0755)
	legacy := config.GlobalConfig{
		Defaults: config.DefaultSettings{GitDomain: "old.com"},
	}
	data, _ := json.MarshalIndent(legacy, "", "  ")
	jsonPath := filepath.Join(configDir, "global.json")
	os.WriteFile(jsonPath, data, 0644)

	// Pre-populate git config (simulating already migrated)
	store := map[string]string{
		"hop.migrated": "true",
	}
	gc := fakeGitConfig(store)
	loader := config.NewGlobalLoaderWithGitConfig(gc)

	loader.Load()

	// global.json should NOT be renamed (migration skipped)
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		t.Error("global.json should still exist (migration should have been skipped)")
	}
}

func TestGetDefaults(t *testing.T) {
	store := map[string]string{}
	gc := fakeGitConfig(store)
	loader := config.NewGlobalLoaderWithGitConfig(gc)
	defs := loader.GetDefaults()

	if defs.Defaults.WorktreeLocation != "{hubPath}/hops/{branch}" {
		t.Errorf("default WorktreeLocation = %q", defs.Defaults.WorktreeLocation)
	}
	if defs.ShellIntegration.Status != "unknown" {
		t.Errorf("default ShellIntegration.Status = %q, want unknown",
			defs.ShellIntegration.Status)
	}
	if defs.Backup.MaxBackups != 3 {
		t.Errorf("default Backup.MaxBackups = %d, want 3", defs.Backup.MaxBackups)
	}
}
