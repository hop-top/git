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

	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// gitconfig.go defaults: bareRepo=false, gitDomain=github.com,
	// autoEnvStart=true, conventionWarning=true
	if cfg.Defaults.BareRepo != false {
		t.Errorf("BareRepo = %v, want false (gitconfig default)", cfg.Defaults.BareRepo)
	}
	if cfg.Defaults.AutoEnvStart != true {
		t.Errorf("AutoEnvStart = %v, want true", cfg.Defaults.AutoEnvStart)
	}
	if cfg.Defaults.GitDomain != "github.com" {
		t.Errorf("GitDomain = %q, want %q", cfg.Defaults.GitDomain, "github.com")
	}
	if cfg.Defaults.ConventionWarning != true {
		t.Errorf("ConventionWarning = %v, want true", cfg.Defaults.ConventionWarning)
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
		"hop.bareRepo":              "false",
		"hop.gitDomain":             "gitlab.com",
		"hop.autoEnvStart":          "false",
		"hop.conventionWarning":     "false",
		"hop.worktreeLocation":      "/custom/{branch}",
		"hop.backup.maxBackups":     "10",
		"hop.backup.enabled":        "false",
		"hop.shellIntegration.status": "approved",
		"hop.shellIntegration.shell":  "zsh",
	}
	gc := fakeGitConfig(store)
	loader := config.NewGlobalLoaderWithGitConfig(gc)

	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Defaults.BareRepo != false {
		t.Errorf("BareRepo = %v, want false", cfg.Defaults.BareRepo)
	}
	if cfg.Defaults.GitDomain != "gitlab.com" {
		t.Errorf("GitDomain = %q, want %q", cfg.Defaults.GitDomain, "gitlab.com")
	}
	if cfg.Defaults.AutoEnvStart != false {
		t.Errorf("AutoEnvStart = %v, want false", cfg.Defaults.AutoEnvStart)
	}
	if cfg.Defaults.WorktreeLocation != "/custom/{branch}" {
		t.Errorf("WorktreeLocation = %q, want %q",
			cfg.Defaults.WorktreeLocation, "/custom/{branch}")
	}
	if cfg.Backup.MaxBackups != 10 {
		t.Errorf("Backup.MaxBackups = %d, want 10", cfg.Backup.MaxBackups)
	}
	if cfg.Backup.Enabled != false {
		t.Errorf("Backup.Enabled = %v, want false", cfg.Backup.Enabled)
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

func TestWriteAndReadRoundTrip(t *testing.T) {
	store := map[string]string{}
	gc := fakeGitConfig(store)
	loader := config.NewGlobalLoaderWithGitConfig(gc)

	original := &config.GlobalConfig{
		Defaults: config.DefaultSettings{
			AutoEnvStart:              false,
			BareRepo:                  false,
			GitDomain:                 "bitbucket.org",
			ConventionWarning:         false,
			WorktreeLocation:          "/my/path/{branch}",
			ShowAllManagedRepos:       true,
			UnusedThresholdDays:       60,
			EnforceCleanForConversion: false,
		},
		ShellIntegration: config.ShellIntegrationSettings{
			Status:         "approved",
			InstalledShell: "fish",
			InstalledPath:  "/home/me/.config/fish/config.fish",
		},
		Backup: config.BackupSettings{
			Enabled:         false,
			KeepBackup:      true,
			MaxBackups:      5,
			CleanupAgeDays:  7,
			PreserveStashes: false,
		},
		Conversion: config.ConversionSettings{
			EnforceClean:    false,
			AllowDirtyForce: true,
			AutoRollback:    false,
		},
	}

	if err := loader.Write(original); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Verify values landed in the fake store
	if store["hop.bareRepo"] != "false" {
		t.Errorf("store[hop.bareRepo] = %q, want %q", store["hop.bareRepo"], "false")
	}
	if store["hop.gitDomain"] != "bitbucket.org" {
		t.Errorf("store[hop.gitDomain] = %q, want %q",
			store["hop.gitDomain"], "bitbucket.org")
	}

	// Read back
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() after Write() error = %v", err)
	}

	if cfg.Defaults.BareRepo != false {
		t.Errorf("roundtrip BareRepo = %v, want false", cfg.Defaults.BareRepo)
	}
	if cfg.Defaults.GitDomain != "bitbucket.org" {
		t.Errorf("roundtrip GitDomain = %q, want %q",
			cfg.Defaults.GitDomain, "bitbucket.org")
	}
	if cfg.Backup.MaxBackups != 5 {
		t.Errorf("roundtrip Backup.MaxBackups = %d, want 5",
			cfg.Backup.MaxBackups)
	}
	if cfg.Conversion.AllowDirtyForce != true {
		t.Errorf("roundtrip Conversion.AllowDirtyForce = %v, want true",
			cfg.Conversion.AllowDirtyForce)
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
			BareRepo:     false,
			AutoEnvStart: false,
			GitDomain:    "gitlab.com",
		},
		ShellIntegration: config.ShellIntegrationSettings{
			Status: "approved",
		},
		Backup: config.BackupSettings{
			Enabled:    true,
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

	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() with migration error = %v", err)
	}

	// Verify scalars migrated to git config
	if cfg.Defaults.BareRepo != false {
		t.Errorf("migrated BareRepo = %v, want false", cfg.Defaults.BareRepo)
	}
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
		"hop.bareRepo": "true",
	}
	gc := fakeGitConfig(store)
	loader := config.NewGlobalLoaderWithGitConfig(gc)

	_, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

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

	if defs.Defaults.BareRepo != true {
		t.Errorf("default BareRepo = %v, want true", defs.Defaults.BareRepo)
	}
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
	if defs.Conversion.AutoRollback != true {
		t.Errorf("default Conversion.AutoRollback = %v, want true",
			defs.Conversion.AutoRollback)
	}
}
