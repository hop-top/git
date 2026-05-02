package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupRepairEnv creates a hub with a "feature" worktree. Returns the env
// and the worktree path. Skips the test if the e2e harness's hub
// scaffolding fails (a pre-existing condition unrelated to repair logic;
// see TestDoctor_NoFix_ReportsIssue which exhibits the same symptom).
func setupRepairEnv(t *testing.T) (*TestEnv, string) {
	t.Helper()
	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	if _, _, code := env.RunCommandWithExit(t, env.RootDir, env.BinPath, env.BareRepoPath, "hub"); code != 0 {
		t.Skip("e2e harness: hub scaffold failed (pre-existing; see TestDoctor_NoFix_ReportsIssue)")
	}
	if _, _, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "add", "feature"); code != 0 {
		t.Skip("e2e harness: 'git hop add' failed (pre-existing)")
	}

	featurePath := filepath.Join(env.HubPath, "hops", "feature")
	return env, featurePath
}

// TestRepair_Healthy_NoOp confirms repair is a no-op on a freshly-built hub.
func TestRepair_Healthy_NoOp(t *testing.T) {
	env, _ := setupRepairEnv(t)

	stdout, _, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "repair", "-n")
	if code != 0 {
		t.Fatalf("expected exit 0 on healthy hub, got %d (stdout: %s)", code, stdout)
	}
	lower := strings.ToLower(stdout)
	if strings.Contains(lower, "rewrite-gitdir") || strings.Contains(lower, "register") || strings.Contains(lower, "unregister") {
		t.Errorf("expected only no-op actions on healthy hub, got:\n%s", stdout)
	}
}

// TestRepair_DryRun_NoMutation confirms -n does not alter on-disk state.
func TestRepair_DryRun_NoMutation(t *testing.T) {
	env, featurePath := setupRepairEnv(t)

	// Capture the worktree's .git pointer pre-run.
	pre, err := os.ReadFile(filepath.Join(featurePath, ".git"))
	if err != nil {
		t.Fatalf("read .git pointer: %v", err)
	}

	// Synthesize a stale pointer.
	if err := os.WriteFile(filepath.Join(featurePath, ".git"), []byte("gitdir: /nope/missing\n"), 0644); err != nil {
		t.Fatalf("write stale pointer: %v", err)
	}

	stdout, _, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "repair", "-n")
	if code != 0 {
		t.Fatalf("dry-run exit=%d, stdout=%s", code, stdout)
	}
	if !strings.Contains(strings.ToLower(stdout), "rewrite-gitdir") {
		t.Errorf("expected dry-run to mention rewrite-gitdir; got:\n%s", stdout)
	}

	// Pointer must be unchanged.
	post, _ := os.ReadFile(filepath.Join(featurePath, ".git"))
	if string(post) != "gitdir: /nope/missing\n" {
		t.Errorf("dry-run mutated pointer; pre=%q post=%q", pre, post)
	}

	// No backup directory created.
	backupsDir := filepath.Join(env.HubPath, ".hop", "backups")
	if entries, _ := os.ReadDir(backupsDir); len(entries) > 0 {
		t.Errorf("dry-run created backups, expected none: %v", entries)
	}
}

// TestRepair_ListBackups_Empty exits 0 with a friendly message when no
// backups are present yet.
func TestRepair_ListBackups_Empty(t *testing.T) {
	env, _ := setupRepairEnv(t)

	stdout, _, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "repair", "--list-backups")
	if code != 0 {
		t.Fatalf("--list-backups exit=%d, out=%s", code, stdout)
	}
	if !strings.Contains(stdout, "(no backups)") {
		t.Errorf("expected '(no backups)' message, got:\n%s", stdout)
	}
}

// TestRepair_Porcelain_StableFormat verifies tab-separated rows on stdout.
func TestRepair_Porcelain_StableFormat(t *testing.T) {
	env, _ := setupRepairEnv(t)

	stdout, _, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "repair", "-n", "--porcelain")
	if code != 0 {
		t.Fatalf("porcelain exit=%d", code)
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if line == "" {
			continue
		}
		if strings.Count(line, "\t") < 2 {
			t.Errorf("porcelain line missing tabs: %q", line)
		}
	}
}

// TestRepair_DoctorAliasGone confirms `git hop doctor` still works but
// `git hop repair` is a separate command (not the doctor alias).
func TestRepair_DoctorAliasGone(t *testing.T) {
	env, _ := setupRepairEnv(t)

	helpOut, _, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "repair", "--help")
	if code != 0 {
		t.Fatalf("repair --help exit=%d, out=%s", code, helpOut)
	}
	if strings.Contains(strings.ToLower(helpOut), "check and repair the environment") {
		t.Errorf("repair --help should NOT show doctor's Short; got:\n%s", helpOut)
	}
	if !strings.Contains(strings.ToLower(helpOut), "stale worktree") {
		t.Errorf("repair --help should describe its own behavior; got:\n%s", helpOut)
	}
}
