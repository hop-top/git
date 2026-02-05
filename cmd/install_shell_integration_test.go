package cmd_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jadb/git-hop/internal/cli"
	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/shell"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

func TestInstallShellIntegrationCmd(t *testing.T) {
	// Create temp config dir
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))
	defer os.Unsetenv("XDG_CONFIG_HOME")

	// Mock shell detection
	os.Setenv("SHELL", "/bin/bash")
	defer os.Unsetenv("SHELL")

	t.Run("successful installation", func(t *testing.T) {
		// Get the command
		installCmd := findCommand(cli.RootCmd, "install-shell-integration")
		if installCmd == nil {
			t.Fatal("install-shell-integration command not found")
		}

		// Execute directly (bypass root command)
		installCmd.Run(installCmd, []string{})

		// Note: If this had returned an error, we would check it,
		// but our command uses output.Fatal which calls os.Exit
		// In a real test we'd need to mock that or refactor

		// Verify wrapper was installed
		fs := afero.NewOsFs()
		rcPath := filepath.Join(tmpDir, ".bashrc")
		if !shell.IsWrapperInstalled(fs, rcPath) {
			t.Error("Wrapper was not installed to RC file")
		}

		// Verify config was updated
		loader := config.NewGlobalLoader()
		cfg, _ := loader.Load()
		if cfg.ShellIntegration.Status != "approved" {
			t.Errorf("Config status = %q, want %q", cfg.ShellIntegration.Status, "approved")
		}
	})

	t.Run("idempotent installation", func(t *testing.T) {
		// Use cli.RootCmd directly
		installCmd := findCommand(cli.RootCmd, "install-shell-integration")

		// Install twice
		installCmd.Run(installCmd, []string{})
		installCmd.Run(installCmd, []string{})

		// Verify still installed
		fs := afero.NewOsFs()
		rcPath := filepath.Join(tmpDir, ".bashrc")
		if !shell.IsWrapperInstalled(fs, rcPath) {
			t.Error("Wrapper not installed after second run")
		}
	})

	t.Run("reinstall after decline", func(t *testing.T) {
		// First set status to declined
		loader := config.NewGlobalLoader()
		cfg, _ := loader.Load()
		cfg.ShellIntegration.Status = "declined"
		loader.Write(cfg)

		// Use cli.RootCmd directly
		installCmd := findCommand(cli.RootCmd, "install-shell-integration")

		// Reinstall
		installCmd.Run(installCmd, []string{})

		// Verify status was updated
		cfg, _ = loader.Load()
		if cfg.ShellIntegration.Status != "approved" {
			t.Errorf("Status = %q, want %q after reinstall", cfg.ShellIntegration.Status, "approved")
		}
	})
}

func TestUninstallShellIntegrationCmd(t *testing.T) {
	// Create temp config dir
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))
	defer os.Unsetenv("XDG_CONFIG_HOME")

	os.Setenv("SHELL", "/bin/bash")
	defer os.Unsetenv("SHELL")

	t.Run("successful uninstall", func(t *testing.T) {
		// Install first
		fs := afero.NewOsFs()
		shell.InstallIntegration(fs)

		// Get uninstall command
		// Use cli.RootCmd directly
		uninstallCmd := findCommand(cli.RootCmd, "uninstall-shell-integration")
		if uninstallCmd == nil {
			t.Fatal("uninstall-shell-integration command not found")
		}

		// Execute
		uninstallCmd.Run(uninstallCmd, []string{})

		// Verify wrapper was removed
		rcPath := filepath.Join(tmpDir, ".bashrc")
		if shell.IsWrapperInstalled(fs, rcPath) {
			t.Error("Wrapper was not removed from RC file")
		}

		// Verify config was updated
		loader := config.NewGlobalLoader()
		cfg, _ := loader.Load()
		if cfg.ShellIntegration.Status != "declined" {
			t.Errorf("Config status = %q, want %q", cfg.ShellIntegration.Status, "declined")
		}
	})

	t.Run("idempotent uninstall", func(t *testing.T) {
		// Use cli.RootCmd directly
		uninstallCmd := findCommand(cli.RootCmd, "uninstall-shell-integration")

		// Uninstall twice (should not error)
		uninstallCmd.Run(uninstallCmd, []string{})
		uninstallCmd.Run(uninstallCmd, []string{})

		// Verify config status
		loader := config.NewGlobalLoader()
		cfg, _ := loader.Load()
		if cfg.ShellIntegration.Status != "declined" {
			t.Errorf("Status = %q, want %q", cfg.ShellIntegration.Status, "declined")
		}
	})
}

// Helper to find a command by name
func findCommand(root interface{ Commands() []*cobra.Command }, name string) *cobra.Command {
	for _, cmd := range root.Commands() {
		if cmd.Name() == name {
			return cmd
		}
	}
	return nil
}
