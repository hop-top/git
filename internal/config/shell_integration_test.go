package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jadb/git-hop/internal/config"
)

func TestShellIntegrationStatus(t *testing.T) {
	// Create temp config dir
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	// Override XDG_CONFIG_HOME for test
	configDir := filepath.Join(tmpDir, ".config", "git-hop")
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))
	defer os.Unsetenv("XDG_CONFIG_HOME")

	loader := config.NewGlobalLoader()

	t.Run("default status is unknown", func(t *testing.T) {
		cfg, err := loader.Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if cfg.ShellIntegration.Status != "unknown" {
			t.Errorf("Default status = %q, want %q", cfg.ShellIntegration.Status, "unknown")
		}
	})

	t.Run("set status to approved", func(t *testing.T) {
		cfg, _ := loader.Load()
		cfg.ShellIntegration.Status = "approved"
		cfg.ShellIntegration.InstalledShell = "bash"
		cfg.ShellIntegration.InstalledPath = filepath.Join(tmpDir, ".bashrc")
		cfg.ShellIntegration.InstalledAt = time.Now()

		if err := loader.Write(cfg); err != nil {
			t.Fatalf("Write() error = %v", err)
		}

		// Verify file was created
		cfgPath := filepath.Join(configDir, "global.json")
		if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
			t.Errorf("Config file was not created at %s", cfgPath)
		}

		// Load and verify
		reloaded, err := loader.Load()
		if err != nil {
			t.Fatalf("Load() after write error = %v", err)
		}

		if reloaded.ShellIntegration.Status != "approved" {
			t.Errorf("Status = %q, want %q", reloaded.ShellIntegration.Status, "approved")
		}
		if reloaded.ShellIntegration.InstalledShell != "bash" {
			t.Errorf("InstalledShell = %q, want %q", reloaded.ShellIntegration.InstalledShell, "bash")
		}
	})

	t.Run("set status to declined", func(t *testing.T) {
		cfg, _ := loader.Load()
		cfg.ShellIntegration.Status = "declined"

		if err := loader.Write(cfg); err != nil {
			t.Fatalf("Write() error = %v", err)
		}

		reloaded, _ := loader.Load()
		if reloaded.ShellIntegration.Status != "declined" {
			t.Errorf("Status = %q, want %q", reloaded.ShellIntegration.Status, "declined")
		}
	})

	t.Run("set status to disabled", func(t *testing.T) {
		cfg, _ := loader.Load()
		cfg.ShellIntegration.Status = "disabled"

		if err := loader.Write(cfg); err != nil {
			t.Fatalf("Write() error = %v", err)
		}

		reloaded, _ := loader.Load()
		if reloaded.ShellIntegration.Status != "disabled" {
			t.Errorf("Status = %q, want %q", reloaded.ShellIntegration.Status, "disabled")
		}
	})
}

func TestShellIntegrationDefaults(t *testing.T) {
	loader := config.NewGlobalLoader()
	defaults := loader.GetDefaults()

	if defaults.ShellIntegration.Status != "unknown" {
		t.Errorf("Default status = %q, want %q", defaults.ShellIntegration.Status, "unknown")
	}

	if defaults.ShellIntegration.InstalledShell != "" {
		t.Errorf("Default InstalledShell = %q, want empty", defaults.ShellIntegration.InstalledShell)
	}
}
