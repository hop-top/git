package hop_test

import (
	"os"
	"path/filepath"
	"testing"

	"hop.top/git/internal/hop"
	"github.com/spf13/afero"
)

func TestUpdateCurrentSymlink(t *testing.T) {
	fs := afero.NewOsFs()

	tests := []struct {
		name           string
		worktreeSubpath string
		setupHub       func(string) error
		expectError    bool
		validateResult func(*testing.T, string, string)
	}{
		{
			name:            "create new symlink",
			worktreeSubpath: "hops/main",
			setupHub: func(hubPath string) error {
				return os.MkdirAll(filepath.Join(hubPath, "hops", "main"), 0755)
			},
			expectError: false,
			validateResult: func(t *testing.T, hubPath, worktreeSubpath string) {
				currentPath := filepath.Join(hubPath, "current")

				// Check symlink exists
				info, err := os.Lstat(currentPath)
				if err != nil {
					t.Fatalf("Symlink does not exist: %v", err)
				}

				// Verify it's a symlink
				if info.Mode()&os.ModeSymlink == 0 {
					t.Error("Current is not a symlink")
				}
			},
		},
		{
			name:            "update existing symlink",
			worktreeSubpath: "hops/feature-b",
			setupHub: func(hubPath string) error {
				// Create initial symlink to feature-a
				if err := os.MkdirAll(filepath.Join(hubPath, "hops", "feature-a"), 0755); err != nil {
					return err
				}
				if err := os.MkdirAll(filepath.Join(hubPath, "hops", "feature-b"), 0755); err != nil {
					return err
				}
				// Create initial symlink
				return hop.UpdateCurrentSymlink(fs, hubPath, filepath.Join(hubPath, "hops", "feature-a"))
			},
			expectError: false,
			validateResult: func(t *testing.T, hubPath, worktreeSubpath string) {
				currentPath := filepath.Join(hubPath, "current")

				// Read symlink target
				target, err := os.Readlink(currentPath)
				if err != nil {
					t.Fatalf("Failed to read symlink: %v", err)
				}

				// Should point to feature-b (relative path)
				if target != worktreeSubpath {
					t.Errorf("Symlink target = %q, want %q", target, worktreeSubpath)
				}
			},
		},
		{
			name:            "worktree with slashes in branch name",
			worktreeSubpath: "hops/feat/my-feature",
			setupHub: func(hubPath string) error {
				return os.MkdirAll(filepath.Join(hubPath, "hops", "feat", "my-feature"), 0755)
			},
			expectError: false,
			validateResult: func(t *testing.T, hubPath, worktreeSubpath string) {
				currentPath := filepath.Join(hubPath, "current")

				target, err := os.Readlink(currentPath)
				if err != nil {
					t.Fatalf("Failed to read symlink: %v", err)
				}

				if target != worktreeSubpath {
					t.Errorf("Symlink target = %q, want %q", target, worktreeSubpath)
				}
			},
		},
		{
			name:            "relative path symlink for portability",
			worktreeSubpath: "hops/main",
			setupHub: func(hubPath string) error {
				return os.MkdirAll(filepath.Join(hubPath, "hops", "main"), 0755)
			},
			expectError: false,
			validateResult: func(t *testing.T, hubPath, worktreeSubpath string) {
				currentPath := filepath.Join(hubPath, "current")

				target, err := os.Readlink(currentPath)
				if err != nil {
					t.Fatalf("Failed to read symlink: %v", err)
				}

				// Should be relative, not absolute
				if filepath.IsAbs(target) {
					t.Errorf("Symlink is absolute (%q), want relative", target)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory for this test
			hubPath := t.TempDir()

			// Setup
			if err := tt.setupHub(hubPath); err != nil {
				t.Fatalf("Setup failed: %v", err)
			}

			worktreePath := filepath.Join(hubPath, tt.worktreeSubpath)

			// Test
			err := hop.UpdateCurrentSymlink(fs, hubPath, worktreePath)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !tt.expectError && tt.validateResult != nil {
				tt.validateResult(t, hubPath, tt.worktreeSubpath)
			}
		})
	}
}

func TestGetCurrentSymlink(t *testing.T) {
	fs := afero.NewOsFs()

	tests := []struct {
		name        string
		setup       func(string) error
		expected    string
		expectError bool
	}{
		{
			name: "no symlink exists",
			setup: func(hubPath string) error {
				return os.MkdirAll(hubPath, 0755)
			},
			expected:    "",
			expectError: true,
		},
		{
			name: "symlink exists",
			setup: func(hubPath string) error {
				os.MkdirAll(filepath.Join(hubPath, "hops", "main"), 0755)
				return hop.UpdateCurrentSymlink(fs, hubPath, filepath.Join(hubPath, "hops", "main"))
			},
			expected:    "hops/main",
			expectError: false,
		},
		{
			name: "symlink with nested path",
			setup: func(hubPath string) error {
				os.MkdirAll(filepath.Join(hubPath, "hops", "feat", "awesome"), 0755)
				return hop.UpdateCurrentSymlink(fs, hubPath, filepath.Join(hubPath, "hops", "feat", "awesome"))
			},
			expected:    "hops/feat/awesome",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory
			hubPath := t.TempDir()

			// Setup
			if err := tt.setup(hubPath); err != nil {
				t.Fatalf("Setup failed: %v", err)
			}

			// Test
			result, err := hop.GetCurrentSymlink(fs, hubPath)

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !tt.expectError && result != tt.expected {
				t.Errorf("GetCurrentSymlink() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestRemoveCurrentSymlink(t *testing.T) {
	fs := afero.NewOsFs()

	t.Run("remove existing symlink", func(t *testing.T) {
		hubPath := t.TempDir()

		// Setup
		os.MkdirAll(filepath.Join(hubPath, "hops", "main"), 0755)
		hop.UpdateCurrentSymlink(fs, hubPath, filepath.Join(hubPath, "hops", "main"))

		// Test
		if err := hop.RemoveCurrentSymlink(fs, hubPath); err != nil {
			t.Fatalf("RemoveCurrentSymlink() error = %v", err)
		}

		// Verify symlink is gone
		currentPath := filepath.Join(hubPath, "current")
		_, err := os.Lstat(currentPath)
		if !os.IsNotExist(err) {
			t.Error("Symlink still exists after removal")
		}
	})

	t.Run("remove non-existent symlink (idempotent)", func(t *testing.T) {
		hubPath := t.TempDir()

		os.MkdirAll(hubPath, 0755)

		// Should not error
		if err := hop.RemoveCurrentSymlink(fs, hubPath); err != nil {
			t.Errorf("RemoveCurrentSymlink() on non-existent = %v, want nil", err)
		}
	})
}
