package shell_test

import (
	"path/filepath"
	"testing"

	"github.com/jadb/git-hop/internal/shell"
	"github.com/spf13/afero"
)

func TestIsWrapperInstalled(t *testing.T) {
	fs := afero.NewMemMapFs()

	tests := []struct {
		name         string
		shellType    string
		setupFile    func(string) error
		expected     bool
	}{
		{
			name:      "no rc file exists",
			shellType: "bash",
			setupFile: func(path string) error { return nil },
			expected:  false,
		},
		{
			name:      "rc file exists but no wrapper",
			shellType: "bash",
			setupFile: func(path string) error {
				return afero.WriteFile(fs, path, []byte("# some other content\nalias foo=bar\n"), 0644)
			},
			expected:  false,
		},
		{
			name:      "wrapper is installed",
			shellType: "bash",
			setupFile: func(path string) error {
				content := `# other stuff
# git-hop shell integration (installed by git-hop)
git-hop() {
    echo "wrapper function"
}
`
				return afero.WriteFile(fs, path, []byte(content), 0644)
			},
			expected:  true,
		},
		{
			name:      "partial match should not count",
			shellType: "bash",
			setupFile: func(path string) error {
				content := `# git-hop is cool but not the wrapper
alias hop="git hop"
`
				return afero.WriteFile(fs, path, []byte(content), 0644)
			},
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp home
			tmpHome := "/tmp/test-home"
			rcPath := filepath.Join(tmpHome, ".bashrc")

			// Setup rc file
			if err := fs.MkdirAll(tmpHome, 0755); err != nil {
				t.Fatalf("Failed to create temp home: %v", err)
			}

			if err := tt.setupFile(rcPath); err != nil {
				t.Fatalf("Failed to setup rc file: %v", err)
			}

			result := shell.IsWrapperInstalled(fs, rcPath)
			if result != tt.expected {
				t.Errorf("IsWrapperInstalled() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestInstallWrapper(t *testing.T) {
	fs := afero.NewMemMapFs()
	tmpHome := "/tmp/test-home"
	rcPath := filepath.Join(tmpHome, ".bashrc")

	// Create home directory
	if err := fs.MkdirAll(tmpHome, 0755); err != nil {
		t.Fatalf("Failed to create temp home: %v", err)
	}

	t.Run("install to non-existent file", func(t *testing.T) {
		shellType := "bash"
		if err := shell.InstallWrapper(fs, shellType, rcPath); err != nil {
			t.Fatalf("InstallWrapper() error = %v", err)
		}

		// Verify file was created
		exists, err := afero.Exists(fs, rcPath)
		if err != nil || !exists {
			t.Error("RC file was not created")
		}

		// Verify wrapper is installed
		if !shell.IsWrapperInstalled(fs, rcPath) {
			t.Error("Wrapper was not installed")
		}
	})

	t.Run("install to existing file", func(t *testing.T) {
		// Reset
		fs = afero.NewMemMapFs()
		fs.MkdirAll(tmpHome, 0755)

		existingContent := "# existing config\nalias foo=bar\n"
		afero.WriteFile(fs, rcPath, []byte(existingContent), 0644)

		shellType := "bash"
		if err := shell.InstallWrapper(fs, shellType, rcPath); err != nil {
			t.Fatalf("InstallWrapper() error = %v", err)
		}

		// Verify wrapper is installed
		if !shell.IsWrapperInstalled(fs, rcPath) {
			t.Error("Wrapper was not installed")
		}

		// Verify existing content is preserved
		content, _ := afero.ReadFile(fs, rcPath)
		if !containsString(string(content), "# existing config") {
			t.Error("Existing content was not preserved")
		}
		if !containsString(string(content), "alias foo=bar") {
			t.Error("Existing aliases were not preserved")
		}
	})

	t.Run("do not install twice", func(t *testing.T) {
		// Reset
		fs = afero.NewMemMapFs()
		fs.MkdirAll(tmpHome, 0755)

		shellType := "bash"

		// First install
		if err := shell.InstallWrapper(fs, shellType, rcPath); err != nil {
			t.Fatalf("First InstallWrapper() error = %v", err)
		}

		// Get content after first install
		firstContent, _ := afero.ReadFile(fs, rcPath)

		// Second install (should be idempotent)
		if err := shell.InstallWrapper(fs, shellType, rcPath); err != nil {
			t.Fatalf("Second InstallWrapper() error = %v", err)
		}

		// Get content after second install
		secondContent, _ := afero.ReadFile(fs, rcPath)

		// Content should be identical (not duplicated)
		if string(firstContent) != string(secondContent) {
			t.Error("Wrapper was installed twice (not idempotent)")
		}
	})
}

func containsString(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsSubstring(s, substr)))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
