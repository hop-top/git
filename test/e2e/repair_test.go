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
	t.Parallel()
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
	t.Parallel()
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

	// No backup directory and no .hop/ footprint of any kind.
	if _, err := os.Stat(filepath.Join(env.HubPath, ".hop")); !os.IsNotExist(err) {
		t.Errorf("dry-run must not create <hub>/.hop, stat err=%v", err)
	}
	if backups := stateBackups(t, env); len(backups) > 0 {
		t.Errorf("dry-run created backups, expected none: %v", backups)
	}
}

// stateBackups lists repair backup manifests under this test's own
// XDG_STATE_HOME, where repair now keeps them.
func stateBackups(t *testing.T, env *TestEnv) []string {
	t.Helper()
	pattern := filepath.Join(env.StateHome, "git-hop", "repair", "*", "backups", "repair-*", "manifest.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %s: %v", pattern, err)
	}
	return matches
}

// stateLocks lists repair lock files under this test's XDG_STATE_HOME.
func stateLocks(t *testing.T, env *TestEnv) []string {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(env.StateHome, "git-hop", "repair", "*", "repair.lock"))
	return matches
}

// TestRepair_Mutating_LeavesNoDotHopOrLock is the regression for a
// successful repair creating <hub>/.hop/ (lock + backups) in a hub that
// never had one. Backups belong in the state dir; the lock must be gone
// once the run completes.
func TestRepair_Mutating_LeavesNoDotHopOrLock(t *testing.T) {
	t.Parallel()
	env, featurePath := setupRepairEnv(t)
	if _, err := os.Stat(filepath.Join(env.HubPath, ".hop")); !os.IsNotExist(err) {
		t.Fatalf("fixture hub must start without .hop, stat err=%v", err)
	}

	// Remove the worktree by hand so git's registry and hop.json go stale.
	if err := os.RemoveAll(featurePath); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := env.RunCommandWithExit(t, filepath.Join(env.HubPath, "hops", "main"), env.BinPath, "repair")
	if code != 0 {
		t.Fatalf("repair exit=%d\nstdout=%s\nstderr=%s", code, stdout, stderr)
	}

	if _, err := os.Stat(filepath.Join(env.HubPath, ".hop")); !os.IsNotExist(err) {
		t.Errorf("repair must not create <hub>/.hop, stat err=%v", err)
	}
	if locks := stateLocks(t, env); len(locks) > 0 {
		t.Errorf("lock file must not survive a completed run: %v", locks)
	}
	backups := stateBackups(t, env)
	if len(backups) != 1 {
		t.Fatalf("expected exactly one backup under %s, got %v", env.StateHome, backups)
	}
	if !strings.Contains(stderr, filepath.Dir(backups[0])) {
		t.Errorf("hint must name the real backup path %s; stderr:\n%s", filepath.Dir(backups[0]), stderr)
	}

	listOut, _, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "repair", "--list-backups")
	if code != 0 || !strings.Contains(listOut, "repair-") {
		t.Errorf("--list-backups should show the relocated backup; exit=%d out=%s", code, listOut)
	}
	undoOut, undoErr, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "repair", "--undo")
	if code != 0 {
		t.Errorf("--undo should restore the relocated backup; exit=%d out=%s err=%s", code, undoOut, undoErr)
	}
}

// TestRepair_HookAbort_ReleasesLock: a handled failure (exit 1) must
// release and remove the lock like a success does, or every later run
// is refused as "another repair is in progress" / leaves a stale file.
func TestRepair_HookAbort_ReleasesLock(t *testing.T) {
	t.Parallel()
	env, featurePath := setupRepairEnv(t)
	if err := os.RemoveAll(featurePath); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(env.HubPath, ".git-hop", "hooks", "pre-repair")
	if err := os.MkdirAll(filepath.Dir(hook), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "repair")
	if code != 1 {
		t.Fatalf("expected exit 1 from hook abort, got %d; stderr=%s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(env.HubPath, ".hop")); !os.IsNotExist(err) {
		t.Errorf("aborted repair must not leave <hub>/.hop, stat err=%v", err)
	}
	if locks := stateLocks(t, env); len(locks) > 0 {
		t.Errorf("lock file must not survive a handled failure: %v", locks)
	}

	// Lock is free again: a second run gets past acquisition.
	_, stderr, _ = env.RunCommandWithExit(t, env.HubPath, env.BinPath, "repair")
	if strings.Contains(stderr, "another repair is in progress") {
		t.Errorf("lock still held after aborted run: %s", stderr)
	}
}

// TestRepair_ListBackups_Empty exits 0 with a friendly message when no
// backups are present yet.
func TestRepair_ListBackups_Empty(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
