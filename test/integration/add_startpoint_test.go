package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"github.com/spf13/afero"
)

// TestAddBranchesFromDefaultBranchTip is the regression test for T-0218: a new
// worktree created via WorktreeManager should branch from the tip of the
// repo's default branch, not from the initial root commit.
func TestAddBranchesFromDefaultBranchTip(t *testing.T) {
	tempDir := t.TempDir()

	// Initialize a real bare-ish repo with two commits on main.
	mustRun(t, tempDir, "git", "init", "-b", "main")
	mustRun(t, tempDir, "git", "config", "user.email", "test@example.com")
	mustRun(t, tempDir, "git", "config", "user.name", "Test User")

	if err := os.WriteFile(filepath.Join(tempDir, "README.md"), []byte("v1"), 0644); err != nil {
		t.Fatalf("write v1: %v", err)
	}
	mustRun(t, tempDir, "git", "add", ".")
	mustRun(t, tempDir, "git", "commit", "-m", "first")

	rootCommit := strings.TrimSpace(runOut(t, tempDir, "git", "rev-parse", "HEAD"))

	if err := os.WriteFile(filepath.Join(tempDir, "README.md"), []byte("v2"), 0644); err != nil {
		t.Fatalf("write v2: %v", err)
	}
	mustRun(t, tempDir, "git", "add", ".")
	mustRun(t, tempDir, "git", "commit", "-m", "second")

	mainTip := strings.TrimSpace(runOut(t, tempDir, "git", "rev-parse", "HEAD"))

	if rootCommit == mainTip {
		t.Fatalf("setup bug: root commit == main tip (%s)", rootCommit)
	}

	// WorktreeManager call: hopspace has no other branches yet, the hub
	// path IS the repo. defaultBranch = "main", startPoint = "" (=> resolves
	// to refs/heads/main since there's no remote configured here).
	fs := afero.NewOsFs()
	g := git.New()
	wm := hop.NewWorktreeManager(fs, g)

	hopspace := &hop.Hopspace{
		Path: tempDir,
		Config: &config.HopspaceConfig{
			Branches: map[string]config.HopspaceBranch{
				"main": {Path: tempDir, Exists: true},
			},
		},
	}

	worktreePath, err := wm.CreateWorktreeTransactional(
		hopspace,
		tempDir,
		"feature-x",
		"{branch}",
		"testorg",
		"testrepo",
		"main",
		"", // startPoint: empty -> resolves to default-branch tip
	)
	if err != nil {
		t.Fatalf("CreateWorktreeTransactional: %v", err)
	}

	// The new worktree's HEAD should match main tip — NOT the root commit.
	got := strings.TrimSpace(runOut(t, worktreePath, "git", "rev-parse", "HEAD"))
	if got != mainTip {
		t.Errorf("new worktree HEAD = %s; want main tip %s (root commit was %s)",
			got, mainTip, rootCommit)
	}
	if got == rootCommit {
		t.Error("new worktree HEAD points at the root commit — the T-0218 bug")
	}
}

// TestAddFromInitialRestoresLegacyBehavior covers --from initial: the new
// worktree must point at the repo's root commit even when main has advanced.
func TestAddFromInitialRestoresLegacyBehavior(t *testing.T) {
	tempDir := t.TempDir()

	mustRun(t, tempDir, "git", "init", "-b", "main")
	mustRun(t, tempDir, "git", "config", "user.email", "test@example.com")
	mustRun(t, tempDir, "git", "config", "user.name", "Test User")

	if err := os.WriteFile(filepath.Join(tempDir, "README.md"), []byte("v1"), 0644); err != nil {
		t.Fatalf("write v1: %v", err)
	}
	mustRun(t, tempDir, "git", "add", ".")
	mustRun(t, tempDir, "git", "commit", "-m", "first")
	rootCommit := strings.TrimSpace(runOut(t, tempDir, "git", "rev-parse", "HEAD"))

	if err := os.WriteFile(filepath.Join(tempDir, "README.md"), []byte("v2"), 0644); err != nil {
		t.Fatalf("write v2: %v", err)
	}
	mustRun(t, tempDir, "git", "add", ".")
	mustRun(t, tempDir, "git", "commit", "-m", "second")

	fs := afero.NewOsFs()
	g := git.New()
	wm := hop.NewWorktreeManager(fs, g)

	hopspace := &hop.Hopspace{
		Path: tempDir,
		Config: &config.HopspaceConfig{
			Branches: map[string]config.HopspaceBranch{
				"main": {Path: tempDir, Exists: true},
			},
		},
	}

	worktreePath, err := wm.CreateWorktreeTransactional(
		hopspace,
		tempDir,
		"legacy-x",
		"{branch}",
		"testorg",
		"testrepo",
		"main",
		"initial",
	)
	if err != nil {
		t.Fatalf("CreateWorktreeTransactional: %v", err)
	}

	got := strings.TrimSpace(runOut(t, worktreePath, "git", "rev-parse", "HEAD"))
	if got != rootCommit {
		t.Errorf("new worktree HEAD = %s; want root commit %s", got, rootCommit)
	}
}

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v in %s: %v\n%s", name, args, dir, err, out)
	}
}

func runOut(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v in %s: %v\n%s", name, args, dir, err, out)
	}
	return string(out)
}
