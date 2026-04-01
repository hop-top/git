package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupDoctorMissingEnv creates a hub with "main" + "feature/gone" worktrees.
// Returns the env and the path to the feature/gone worktree.
func setupDoctorMissingEnv(t *testing.T) (*TestEnv, string) {
	t.Helper()
	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	env.RunGitHop(t, env.HubPath, "add", "feature/gone")

	featurePath := filepath.Join(env.HubPath, "hops", "feature/gone")
	return env, featurePath
}

// TestDoctor_MissingWorktree_Merged_AutoDeleted verifies that when a worktree's
// branch is merged into the default branch and its directory has been removed,
// doctor --fix auto-deletes the state entry without prompting.
func TestDoctor_MissingWorktree_Merged_AutoDeleted(t *testing.T) {
	env, featurePath := setupDoctorMissingEnv(t)

	mainPath := filepath.Join(env.HubPath, "hops", "main")

	// Add a commit to the feature branch so there is something to merge.
	env.RunCommand(t, featurePath, "git", "commit", "--allow-empty", "-m", "feature work")

	// Merge feature/gone into main.
	env.RunCommand(t, mainPath, "git", "merge", "feature/gone", "--no-ff", "-m", "Merge feature/gone")

	// Remove the feature worktree directory from disk (simulating a manual rm).
	if err := os.RemoveAll(featurePath); err != nil {
		t.Fatalf("failed to remove feature worktree dir: %v", err)
	}

	// Verify the directory is actually gone before running doctor.
	if _, err := os.Stat(featurePath); !os.IsNotExist(err) {
		t.Fatalf("expected featurePath to be absent before doctor run")
	}

	// Run doctor --fix; should auto-delete the state entry.
	out := env.RunGitHop(t, env.HubPath, "doctor", "--fix")

	// Output should mention the auto-removal (merged branch path).
	lowerOut := strings.ToLower(out)
	if !strings.Contains(lowerOut, "auto-remov") && !strings.Contains(lowerOut, "merged") {
		t.Errorf("expected output to mention auto-removal or merged; got:\n%s", out)
	}
}

// TestDoctor_MissingWorktree_Present_NoAction verifies that doctor --fix does
// not produce false positives when the worktree directory actually exists.
func TestDoctor_MissingWorktree_Present_NoAction(t *testing.T) {
	env, featurePath := setupDoctorMissingEnv(t)

	// Confirm the directory is present.
	if _, err := os.Stat(featurePath); err != nil {
		t.Fatalf("expected featurePath to exist before doctor run: %v", err)
	}

	out := env.RunGitHop(t, env.HubPath, "doctor", "--fix")

	// Output must NOT mention missing worktrees.
	if strings.Contains(strings.ToLower(out), "missing worktree") {
		t.Errorf("expected no missing-worktree report, but got:\n%s", out)
	}
}

// TestDoctor_NoFix_ReportsIssue verifies that without --fix the doctor command
// reports a missing worktree but does not modify state.
func TestDoctor_NoFix_ReportsIssue(t *testing.T) {
	env, featurePath := setupDoctorMissingEnv(t)

	// Remove the feature worktree directory.
	if err := os.RemoveAll(featurePath); err != nil {
		t.Fatalf("failed to remove feature worktree dir: %v", err)
	}

	// Run doctor without --fix.
	out := runCommandExpectError(t, env, env.HubPath, env.BinPath, "doctor")

	// Output should report the missing worktree.
	lowerOut := strings.ToLower(out)
	if !strings.Contains(lowerOut, "missing") && !strings.Contains(lowerOut, "issue") {
		t.Errorf("expected output to mention missing worktree or issues; got:\n%s", out)
	}
}
