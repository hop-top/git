package e2e_test

import (
	"os"
	"path/filepath"
	"testing"

	"hop.top/git/internal/hop"
	"github.com/spf13/afero"
)

func TestCurrentSymlinkOnHop(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	// Create test environment
	tmpDir := t.TempDir()
	hubPath := filepath.Join(tmpDir, "test-repo")

	// Initialize a bare repo with worktrees
	fs := afero.NewOsFs()
	setupBareRepoWithWorktrees(t, fs, hubPath, []string{"main", "feature-a", "feature-b"})

	// Navigate to main worktree
	mainWorktree := filepath.Join(hubPath, "hops", "main")
	os.Chdir(mainWorktree)

	t.Run("hop to branch creates current symlink", func(t *testing.T) {
		// Simulate hopping to feature-a
		featureAPath := filepath.Join(hubPath, "hops", "feature-a")

		// Update current symlink (this is what the hop command should do)
		err := hop.UpdateCurrentSymlink(fs, hubPath, featureAPath)
		if err != nil {
			t.Fatalf("UpdateCurrentSymlink failed: %v", err)
		}

		// Verify symlink was created
		currentPath := filepath.Join(hubPath, "current")
		target, err := os.Readlink(currentPath)
		if err != nil {
			t.Fatalf("Failed to read current symlink: %v", err)
		}

		expectedTarget := "hops/feature-a"
		if target != expectedTarget {
			t.Errorf("Symlink target = %q, want %q", target, expectedTarget)
		}
	})

	t.Run("hop to another branch updates current symlink", func(t *testing.T) {
		// First hop to feature-a
		featureAPath := filepath.Join(hubPath, "hops", "feature-a")
		hop.UpdateCurrentSymlink(fs, hubPath, featureAPath)

		// Then hop to feature-b
		featureBPath := filepath.Join(hubPath, "hops", "feature-b")
		err := hop.UpdateCurrentSymlink(fs, hubPath, featureBPath)
		if err != nil {
			t.Fatalf("UpdateCurrentSymlink failed: %v", err)
		}

		// Verify symlink was updated
		currentPath := filepath.Join(hubPath, "current")
		target, err := os.Readlink(currentPath)
		if err != nil {
			t.Fatalf("Failed to read current symlink: %v", err)
		}

		expectedTarget := "hops/feature-b"
		if target != expectedTarget {
			t.Errorf("Symlink target = %q, want %q", target, expectedTarget)
		}
	})

	t.Run("current symlink always points to last hopped worktree", func(t *testing.T) {
		branches := []string{"main", "feature-a", "feature-b", "main"}

		for _, branch := range branches {
			worktreePath := filepath.Join(hubPath, "hops", branch)
			hop.UpdateCurrentSymlink(fs, hubPath, worktreePath)

			// Verify current points to this branch
			target, _ := hop.GetCurrentSymlink(fs, hubPath)
			expected := filepath.Join("hops", branch)

			if target != expected {
				t.Errorf("After hopping to %s, current = %q, want %q", branch, target, expected)
			}
		}
	})
}

// Helper to set up a bare repo with multiple worktrees for testing
func setupBareRepoWithWorktrees(t *testing.T, fs afero.Fs, hubPath string, branches []string) {
	t.Helper()

	// Create hub directory structure
	if err := os.MkdirAll(filepath.Join(hubPath, ".git"), 0755); err != nil {
		t.Fatalf("Failed to create .git: %v", err)
	}

	// Create hops directory
	hopsDir := filepath.Join(hubPath, "hops")
	if err := os.MkdirAll(hopsDir, 0755); err != nil {
		t.Fatalf("Failed to create hops dir: %v", err)
	}

	// Create worktree directories
	for _, branch := range branches {
		worktreePath := filepath.Join(hopsDir, branch)
		if err := os.MkdirAll(worktreePath, 0755); err != nil {
			t.Fatalf("Failed to create worktree %s: %v", branch, err)
		}

		// Create a dummy file to make it look like a real worktree
		dummyFile := filepath.Join(worktreePath, "README.md")
		if err := os.WriteFile(dummyFile, []byte("# "+branch), 0644); err != nil {
			t.Fatalf("Failed to create dummy file: %v", err)
		}
	}

	// Create hop.json config
	hopConfig := `{
  "repo": {
    "uri": "git@github.com:test/repo.git",
    "org": "test",
    "repo": "repo",
    "defaultBranch": "main",
    "structure": "bare-worktree",
    "isBare": true
  },
  "branches": {
    "main": {"path": "` + filepath.Join(hopsDir, "main") + `"},
    "feature-a": {"path": "` + filepath.Join(hopsDir, "feature-a") + `"},
    "feature-b": {"path": "` + filepath.Join(hopsDir, "feature-b") + `"}
  }
}`

	configPath := filepath.Join(hubPath, "hop.json")
	if err := os.WriteFile(configPath, []byte(hopConfig), 0644); err != nil {
		t.Fatalf("Failed to create hop.json: %v", err)
	}
}
