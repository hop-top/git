package config_test

import (
	"testing"
	"time"

	"hop.top/git/internal/config"
)

func TestShellIntegrationStatus(t *testing.T) {
	store := map[string]string{}
	gc := fakeGitConfig(store)
	loader := config.NewGlobalLoaderWithGitConfig(gc)

	t.Run("default status is unknown", func(t *testing.T) {
		cfg, err := loader.Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if cfg.ShellIntegration.Status != "unknown" {
			t.Errorf("Default status = %q, want %q",
				cfg.ShellIntegration.Status, "unknown")
		}
	})

	t.Run("set status to approved", func(t *testing.T) {
		cfg, _ := loader.Load()
		cfg.ShellIntegration.Status = "approved"
		cfg.ShellIntegration.InstalledShell = "bash"
		cfg.ShellIntegration.InstalledPath = "/tmp/.bashrc"
		cfg.ShellIntegration.InstalledAt = time.Now()

		if err := loader.Write(cfg); err != nil {
			t.Fatalf("Write() error = %v", err)
		}

		// Verify values persisted to git config store
		if store["hop.shellIntegration.status"] != "approved" {
			t.Errorf("store status = %q, want %q",
				store["hop.shellIntegration.status"], "approved")
		}

		// Load and verify
		reloaded, err := loader.Load()
		if err != nil {
			t.Fatalf("Load() after write error = %v", err)
		}

		if reloaded.ShellIntegration.Status != "approved" {
			t.Errorf("Status = %q, want %q",
				reloaded.ShellIntegration.Status, "approved")
		}
		if reloaded.ShellIntegration.InstalledShell != "bash" {
			t.Errorf("InstalledShell = %q, want %q",
				reloaded.ShellIntegration.InstalledShell, "bash")
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
			t.Errorf("Status = %q, want %q",
				reloaded.ShellIntegration.Status, "declined")
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
			t.Errorf("Status = %q, want %q",
				reloaded.ShellIntegration.Status, "disabled")
		}
	})
}

func TestShellIntegrationDefaults(t *testing.T) {
	store := map[string]string{}
	gc := fakeGitConfig(store)
	loader := config.NewGlobalLoaderWithGitConfig(gc)
	defaults := loader.GetDefaults()

	if defaults.ShellIntegration.Status != "unknown" {
		t.Errorf("Default status = %q, want %q",
			defaults.ShellIntegration.Status, "unknown")
	}

	if defaults.ShellIntegration.InstalledShell != "" {
		t.Errorf("Default InstalledShell = %q, want empty",
			defaults.ShellIntegration.InstalledShell)
	}
}
