package shell

import (
	"fmt"
	"os"
	"time"

	"hop.top/git/internal/config"
	"github.com/spf13/afero"
)

// IntegrationResult contains information about the shell integration setup
type IntegrationResult struct {
	Status         string
	InstalledShell string
	RcPath         string
	Message        string
}

// ShouldPromptForSetup determines if we should prompt the user to set up shell integration
func ShouldPromptForSetup(cfg *config.GlobalConfig, fs afero.Fs) bool {
	// Don't prompt if already wrapped
	if os.Getenv("HOP_WRAPPER_ACTIVE") == "1" {
		return false
	}

	// Don't prompt if non-interactive
	if !IsInteractive() {
		return false
	}

	status := cfg.ShellIntegration.Status

	// Don't prompt if user has already declined or disabled
	if status == "declined" || status == "disabled" {
		return false
	}

	// Prompt if status is unknown
	if status == "unknown" {
		return true
	}

	// If status is approved, check if wrapper is actually installed
	if status == "approved" {
		shellType := cfg.ShellIntegration.InstalledShell
		if shellType == "" {
			shellType = DetectShell()
		}
		rcPath := cfg.ShellIntegration.InstalledPath
		if rcPath == "" {
			rcPath = GetRcFile(shellType)
		}

		// Prompt if not actually installed (maybe user removed it)
		return !IsWrapperInstalled(fs, rcPath)
	}

	return false
}

// InstallIntegration installs the shell wrapper function and updates config
func InstallIntegration(fs afero.Fs) (*IntegrationResult, error) {
	// Detect shell
	shellType := DetectShell()
	if shellType == "unknown" {
		return nil, fmt.Errorf("unsupported shell (detected: %s)", os.Getenv("SHELL"))
	}

	// Get RC file path
	rcPath := GetRcFile(shellType)

	// Install wrapper
	if err := InstallWrapper(fs, shellType, rcPath); err != nil {
		return nil, fmt.Errorf("failed to install wrapper: %w", err)
	}

	// Update config
	loader := config.NewGlobalLoader()
	cfg, err := loader.Load()
	if err != nil {
		cfg = loader.GetDefaults()
	}

	cfg.ShellIntegration.Status = "approved"
	cfg.ShellIntegration.InstalledShell = shellType
	cfg.ShellIntegration.InstalledPath = rcPath
	cfg.ShellIntegration.InstalledAt = time.Now()

	if err := loader.Write(cfg); err != nil {
		return nil, fmt.Errorf("failed to update config: %w", err)
	}

	return &IntegrationResult{
		Status:         "approved",
		InstalledShell: shellType,
		RcPath:         rcPath,
		Message:        fmt.Sprintf("Shell integration installed to %s", rcPath),
	}, nil
}

// UninstallIntegration removes the shell wrapper function and updates config
func UninstallIntegration(fs afero.Fs) error {
	// Load config to get installed shell info
	loader := config.NewGlobalLoader()
	cfg, err := loader.Load()
	if err != nil {
		cfg = loader.GetDefaults()
	}

	// Get shell and RC path
	shellType := cfg.ShellIntegration.InstalledShell
	if shellType == "" {
		shellType = DetectShell()
	}

	rcPath := cfg.ShellIntegration.InstalledPath
	if rcPath == "" {
		rcPath = GetRcFile(shellType)
	}

	// Remove wrapper (idempotent)
	if err := UninstallWrapper(fs, rcPath); err != nil {
		// Don't fail if file doesn't exist
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to uninstall wrapper: %w", err)
		}
	}

	// Update config status
	cfg.ShellIntegration.Status = "declined"

	if err := loader.Write(cfg); err != nil {
		return fmt.Errorf("failed to update config: %w", err)
	}

	return nil
}

// SetIntegrationStatus updates the shell integration status in global config
func SetIntegrationStatus(status string) error {
	loader := config.NewGlobalLoader()
	cfg, err := loader.Load()
	if err != nil {
		cfg = loader.GetDefaults()
	}

	cfg.ShellIntegration.Status = status

	if err := loader.Write(cfg); err != nil {
		return fmt.Errorf("failed to update config: %w", err)
	}

	return nil
}
