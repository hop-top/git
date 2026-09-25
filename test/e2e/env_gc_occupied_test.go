package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A regular file where a branch's worktree directory should be made
// `env gc` abort: looking for package-manager files below it failed with
// ENOTDIR, fatal for the whole collection. The occupied worktree is now
// skipped with a note, and the rest are still collected: an orphaned
// dependency directory is found under --dry-run and deleted after.
func TestEnvGC_FileAtWorktreePath_SkippedAndCollectionContinues(t *testing.T) {
	t.Parallel()
	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	env.RunGitHop(t, env.HubPath, "add", "feature/occupied")

	occupied := filepath.Join(env.HubPath, "hops", "feature/occupied")
	if err := os.RemoveAll(occupied); err != nil {
		t.Fatalf("remove worktree dir: %v", err)
	}
	WriteFile(t, occupied, "not a worktree")

	// An orphan no worktree links to: deps store of the local hopspace.
	depsStore := filepath.Join(env.HubPath, "deps")
	orphanKey := "node_modules.0rphan"
	orphan := filepath.Join(depsStore, orphanKey)
	if err := os.MkdirAll(filepath.Join(orphan, "pkg"), 0o755); err != nil {
		t.Fatalf("mkdir deps: %v", err)
	}
	WriteFile(t, filepath.Join(orphan, "pkg", "index.js"), "module.exports = 1\n")
	WriteFile(t, filepath.Join(depsStore, ".registry.json"),
		`{"entries":{"`+orphanKey+`":{"lockfileHash":"0rphan","lockfilePath":"package-lock.json","usedBy":["feature/occupied"]}}}`)

	stdout, stderr, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "env", "gc", "--dry-run")
	out := stdout + stderr
	if code != 0 {
		t.Fatalf("env gc --dry-run exit = %d, want 0; output:\n%s", code, out)
	}
	if !strings.Contains(out, "Skipping feature/occupied: worktree path is not a directory: ") {
		t.Errorf("expected a note skipping the occupied worktree; output:\n%s", out)
	}
	if !strings.Contains(out, orphanKey) {
		t.Errorf("expected the orphan %s to be listed; output:\n%s", orphanKey, out)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Fatalf("--dry-run must not delete the orphan: %v", err)
	}

	stdout, stderr, code = env.RunCommandWithExit(t, env.HubPath, env.BinPath, "env", "gc", "--no-prompt")
	out = stdout + stderr
	if code != 0 {
		t.Fatalf("env gc --no-prompt exit = %d, want 0; output:\n%s", code, out)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("orphan %s should be deleted, stat err = %v; output:\n%s", orphan, err, out)
	}
	data, err := os.ReadFile(occupied)
	if err != nil || string(data) != "not a worktree" {
		t.Errorf("the file at the worktree path is left alone; got %q, %v", data, err)
	}
}
