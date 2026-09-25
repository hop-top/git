package e2e

import (
	"encoding/json"
	"hop.top/git/internal/state"
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
	t.Parallel()
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

	// Run doctor --fix; should auto-delete the state entry. Every issue is
	// fixed, so it exits 0.
	stdout, stderr, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "doctor", "--fix")
	out := stdout + stderr
	if code != 0 {
		t.Errorf("doctor --fix exit = %d, want 0; output:\n%s", code, out)
	}

	// Output should mention the auto-removal (merged branch path).
	lowerOut := strings.ToLower(out)
	if !strings.Contains(lowerOut, "auto-remov") && !strings.Contains(lowerOut, "merged") {
		t.Errorf("expected output to mention auto-removal or merged; got:\n%s", out)
	}

	// Cleaned up, not recreated: no directory, no hop.json row, no state
	// entry, and no stale git registration left to block a later add.
	if _, err := os.Stat(featurePath); !os.IsNotExist(err) {
		t.Errorf("merged branch's worktree must not be recreated at %s", featurePath)
	}
	if hopJSONHasBranch(t, env, "feature/gone") {
		t.Error("hop.json must no longer list feature/gone")
	}
	if stateHasWorktree(t, env, "feature/gone") {
		t.Error("state must no longer list feature/gone")
	}
	if worktreeRegistered(t, env, featurePath) {
		t.Errorf("git must no longer register %s", featurePath)
	}
}

// commitWork commits a new file in the worktree at dir. Unmerged work
// needs content: a branch whose only commits are empty adds nothing to
// default, so doctor (like remove and status) counts it as merged.
func commitWork(t *testing.T, env *TestEnv, dir, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte(msg+"\n"), 0o644); err != nil {
		t.Fatalf("write work.txt: %v", err)
	}
	env.RunCommand(t, dir, "git", "add", "work.txt")
	env.RunCommand(t, dir, "git", "commit", "-m", msg)
}

// TestDoctor_MissingWorktree_Unmerged_Recreated covers a worktree removed
// by hand whose branch carries unmerged work: git still registers the
// directory, so recreating it means clearing that stale registration
// first. The branch's commits come back checked out, and doctor exits 0.
func TestDoctor_MissingWorktree_Unmerged_Recreated(t *testing.T) {
	t.Parallel()
	env, featurePath := setupDoctorMissingEnv(t)

	commitWork(t, env, featurePath, "unmerged work")
	if err := os.RemoveAll(featurePath); err != nil {
		t.Fatalf("failed to remove feature worktree dir: %v", err)
	}

	stdout, stderr, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "doctor", "--fix")
	if code != 0 {
		t.Errorf("doctor --fix exit = %d, want 0; output:\n%s%s", code, stdout, stderr)
	}

	if _, err := os.Stat(featurePath); err != nil {
		t.Fatalf("unmerged branch's worktree must be recreated at %s: %v", featurePath, err)
	}
	if got := strings.TrimSpace(env.RunCommand(t, featurePath, "git", "log", "-1", "--format=%s")); got != "unmerged work" {
		t.Errorf("recreated worktree HEAD = %q, want the branch's unmerged commit", got)
	}
	if !worktreeRegistered(t, env, featurePath) {
		t.Errorf("git must register the recreated worktree at %s", featurePath)
	}
	if !hopJSONHasBranch(t, env, "feature/gone") {
		t.Error("hop.json must keep feature/gone")
	}
	if !stateHasWorktree(t, env, "feature/gone") {
		t.Error("state must keep feature/gone")
	}
}

// TestDoctor_MissingWorktree_DryRun previews both outcomes. The preview
// follows the real run's logic and exit status, and changes nothing: the
// directory stays gone, git keeps its registration, hop.json keeps the row.
func TestDoctor_MissingWorktree_DryRun(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		merged    bool
		wantFixes []string // would-fix messages expected for the branch
		rejectFix string   // would-fix message that must not appear
	}{
		{"merged", true, []string{"clear stale worktree registration", "prune hop.json entry"}, "recreate worktree"},
		{"unmerged", false, []string{"clear stale worktree registration", "recreate worktree"}, "prune hop.json entry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env, featurePath := setupDoctorMissingEnv(t)
			commitWork(t, env, featurePath, "work")
			if tc.merged {
				mainPath := filepath.Join(env.HubPath, "hops", "main")
				env.RunCommand(t, mainPath, "git", "merge", "feature/gone", "--no-ff", "-m", "Merge feature/gone")
			}
			if err := os.RemoveAll(featurePath); err != nil {
				t.Fatalf("failed to remove feature worktree dir: %v", err)
			}

			stdout, stderr, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath,
				"doctor", "--fix", "--dry-run", "--format", "json")
			if code != 0 {
				t.Errorf("doctor --fix --dry-run exit = %d, want 0; output:\n%s%s", code, stdout, stderr)
			}
			var records []struct{ Kind, Check, Subject, Message string }
			if err := json.Unmarshal([]byte(stdout), &records); err != nil {
				t.Fatalf("parse records: %v\n%s", err, stdout)
			}
			var fixes []string
			for _, rec := range records {
				// A registration record's subject is the worktree path, as
				// doctor resolved it from its working directory.
				if rec.Kind == "would-fix" && (rec.Subject == "feature/gone" ||
					strings.HasSuffix(rec.Subject, string(filepath.Separator)+filepath.Join("hub", "hops", "feature", "gone"))) {
					fixes = append(fixes, rec.Message)
				}
			}
			joined := strings.Join(fixes, "\n")
			for _, want := range tc.wantFixes {
				if !strings.Contains(joined, want) {
					t.Errorf("missing would-fix %q; got:\n%s", want, joined)
				}
			}
			if strings.Contains(joined, tc.rejectFix) {
				t.Errorf("unexpected would-fix %q; got:\n%s", tc.rejectFix, joined)
			}

			if _, err := os.Stat(featurePath); !os.IsNotExist(err) {
				t.Errorf("dry-run must not recreate %s", featurePath)
			}
			if !worktreeRegistered(t, env, featurePath) {
				t.Error("dry-run must leave git's registration alone")
			}
			if !hopJSONHasBranch(t, env, "feature/gone") {
				t.Error("dry-run must leave hop.json alone")
			}
		})
	}
}

// TestDoctor_MissingWorktree_Present_NoAction verifies that doctor --fix does
// not produce false positives when the worktree directory actually exists.
func TestDoctor_MissingWorktree_Present_NoAction(t *testing.T) {
	t.Parallel()
	env, featurePath := setupDoctorMissingEnv(t)

	// Confirm the directory is present, and give it content git does not
	// track, which any removal would lose.
	if _, err := os.Stat(featurePath); err != nil {
		t.Fatalf("expected featurePath to exist before doctor run: %v", err)
	}
	untracked := filepath.Join(featurePath, "notes.txt")
	WriteFile(t, untracked, "keep me")

	out := env.RunGitHop(t, env.HubPath, "doctor", "--fix")

	// Output must NOT mention missing worktrees.
	if strings.Contains(strings.ToLower(out), "missing worktree") {
		t.Errorf("expected no missing-worktree report, but got:\n%s", out)
	}
	if _, err := os.Stat(untracked); err != nil {
		t.Errorf("doctor --fix must leave a present worktree untouched: %v", err)
	}
	if !worktreeRegistered(t, env, featurePath) {
		t.Errorf("doctor --fix must keep git's registration of %s", featurePath)
	}
	if !hopJSONHasBranch(t, env, "feature/gone") {
		t.Error("doctor --fix must keep hop.json's feature/gone row")
	}
}

// TestDoctor_NoFix_ReportsIssue verifies that without --fix the doctor command
// reports a missing worktree but does not modify state.
func TestDoctor_NoFix_ReportsIssue(t *testing.T) {
	t.Parallel()
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

// hopJSONHasBranch reports whether the hub's hop.json lists branch.
func hopJSONHasBranch(t *testing.T, env *TestEnv, branch string) bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(env.HubPath, "hop.json"))
	if err != nil {
		t.Fatalf("read hop.json: %v", err)
	}
	var cfg struct {
		Branches map[string]json.RawMessage `json:"branches"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse hop.json: %v", err)
	}
	_, ok := cfg.Branches[branch]
	return ok
}

// stateHasWorktree reports whether any repository in state lists branch.
func stateHasWorktree(t *testing.T, env *TestEnv, branch string) bool {
	t.Helper()
	st, err := env.LoadState(t)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if st == nil {
		return false
	}
	for _, repo := range st.Repositories {
		if recordsBranch(repo, branch) {
			return true
		}
	}
	return false
}

// recordsBranch reports whether repo records a worktree of branch, in
// any hub.
func recordsBranch(repo *state.RepositoryState, branch string) bool {
	for _, wt := range repo.Worktrees {
		if wt != nil && wt.Branch == branch {
			return true
		}
	}
	return false
}

// worktreeRegistered reports whether git lists path among the hub's
// worktrees. git records resolved paths, and the test root may sit under
// a symlink (macOS /var -> /private/var), so both sides are compared with
// the root resolved.
func worktreeRegistered(t *testing.T, env *TestEnv, path string) bool {
	t.Helper()
	root, err := filepath.EvalSymlinks(env.RootDir)
	if err != nil {
		t.Fatalf("resolve test root: %v", err)
	}
	resolve := func(p string) string {
		p = filepath.Clean(p)
		if rest, ok := strings.CutPrefix(p, env.RootDir+string(filepath.Separator)); ok {
			return filepath.Join(root, rest)
		}
		return p
	}
	want := resolve(path)
	out := env.RunCommand(t, env.HubPath, "git", "worktree", "list", "--porcelain")
	for _, line := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok && resolve(p) == want {
			return true
		}
	}
	return false
}
