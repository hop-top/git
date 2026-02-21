package shell_test

import (
	"os"
	"path/filepath"
	"testing"

	"hop.top/git/internal/config"
	"hop.top/git/internal/shell"
	"github.com/spf13/afero"
)

func TestShouldPromptForSetup(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		cfgSetup func(*config.GlobalConfig)
		expected bool
	}{
		{
			name:    "already wrapped - should not prompt",
			envVars: map[string]string{"HOP_WRAPPER_ACTIVE": "1"},
			cfgSetup: func(cfg *config.GlobalConfig) {
				cfg.ShellIntegration.Status = "unknown"
			},
			expected: false,
		},
		{
			name:    "non-interactive - should not prompt",
			envVars: map[string]string{"CI": "true"},
			cfgSetup: func(cfg *config.GlobalConfig) {
				cfg.ShellIntegration.Status = "unknown"
			},
			expected: false,
		},
		{
			name:    "status declined - should not prompt",
			envVars: map[string]string{},
			cfgSetup: func(cfg *config.GlobalConfig) {
				cfg.ShellIntegration.Status = "declined"
			},
			expected: false,
		},
		{
			name:    "status disabled - should not prompt",
			envVars: map[string]string{},
			cfgSetup: func(cfg *config.GlobalConfig) {
				cfg.ShellIntegration.Status = "disabled"
			},
			expected: false,
		},
		{
			name:    "status unknown - should prompt",
			envVars: map[string]string{},
			cfgSetup: func(cfg *config.GlobalConfig) {
				cfg.ShellIntegration.Status = "unknown"
			},
			expected: true,
		},
		{
			name:    "status approved but not installed - should prompt",
			envVars: map[string]string{},
			cfgSetup: func(cfg *config.GlobalConfig) {
				cfg.ShellIntegration.Status = "approved"
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore env vars
			savedEnv := make(map[string]string)
			for k := range tt.envVars {
				savedEnv[k] = os.Getenv(k)
			}
			defer func() {
				for k, v := range savedEnv {
					if v == "" {
						os.Unsetenv(k)
					} else {
						os.Setenv(k, v)
					}
				}
			}()

			// Set test env vars
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			// Create test config
			cfg := config.NewGlobalLoader().GetDefaults()
			tt.cfgSetup(cfg)

			// Create mock RC file for approved status test
			fs := afero.NewMemMapFs()
			rcPath := "/tmp/.bashrc"
			if cfg.ShellIntegration.Status == "approved" {
				// Simulate wrapper NOT being installed
				afero.WriteFile(fs, rcPath, []byte("# some content\n"), 0644)
			}

			result := shell.ShouldPromptForSetup(cfg, fs)
			if result != tt.expected {
				t.Errorf("ShouldPromptForSetup() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestInstallIntegration(t *testing.T) {
	// Create temp config dir
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	// Override XDG_CONFIG_HOME for test
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))
	defer os.Unsetenv("XDG_CONFIG_HOME")

	// Mock shell detection
	os.Setenv("SHELL", "/bin/bash")
	defer os.Unsetenv("SHELL")

	fs := afero.NewOsFs()

	t.Run("successful installation", func(t *testing.T) {
		result, err := shell.InstallIntegration(fs)
		if err != nil {
			t.Fatalf("InstallIntegration() error = %v", err)
		}

		if result.Status != "approved" {
			t.Errorf("Status = %q, want %q", result.Status, "approved")
		}

		if result.InstalledShell != "bash" {
			t.Errorf("InstalledShell = %q, want %q", result.InstalledShell, "bash")
		}

		if result.RcPath == "" {
			t.Error("RcPath is empty")
		}

		// Verify wrapper was installed
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
		// Install twice
		first, _ := shell.InstallIntegration(fs)
		second, _ := shell.InstallIntegration(fs)

		if first.Status != second.Status {
			t.Error("Second installation changed status")
		}

		// Verify wrapper is not duplicated
		rcPath := filepath.Join(tmpDir, ".bashrc")
		content, _ := afero.ReadFile(fs, rcPath)
		firstIdx := -1
		secondIdx := -1
		marker := "git-hop shell integration"

		for i := 0; i < len(content)-len(marker); i++ {
			if string(content[i:i+len(marker)]) == marker {
				if firstIdx == -1 {
					firstIdx = i
				} else if secondIdx == -1 {
					secondIdx = i
					break
				}
			}
		}

		if secondIdx != -1 {
			t.Error("Wrapper was installed twice (not idempotent)")
		}
	})
}

func TestUninstallIntegration(t *testing.T) {
	// Create temp config dir
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))
	defer os.Unsetenv("XDG_CONFIG_HOME")

	os.Setenv("SHELL", "/bin/bash")
	defer os.Unsetenv("SHELL")

	fs := afero.NewOsFs()

	// Install first
	shell.InstallIntegration(fs)

	t.Run("successful uninstall", func(t *testing.T) {
		if err := shell.UninstallIntegration(fs); err != nil {
			t.Fatalf("UninstallIntegration() error = %v", err)
		}

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
		// Uninstall when already uninstalled
		if err := shell.UninstallIntegration(fs); err != nil {
			t.Fatalf("Second UninstallIntegration() error = %v", err)
		}
	})
}

func TestSetIntegrationStatus(t *testing.T) {
	tmpDir := t.TempDir()
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", originalHome)

	os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))
	defer os.Unsetenv("XDG_CONFIG_HOME")

	tests := []struct {
		name   string
		status string
	}{
		{"set to approved", "approved"},
		{"set to declined", "declined"},
		{"set to disabled", "disabled"},
		{"set to unknown", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := shell.SetIntegrationStatus(tt.status); err != nil {
				t.Fatalf("SetIntegrationStatus(%q) error = %v", tt.status, err)
			}

			// Verify status was saved
			loader := config.NewGlobalLoader()
			cfg, err := loader.Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			if cfg.ShellIntegration.Status != tt.status {
				t.Errorf("Status = %q, want %q", cfg.ShellIntegration.Status, tt.status)
			}
		})
	}
}
