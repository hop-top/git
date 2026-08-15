package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Abort paths -----------------------------------------------------
//
// A pre- hook exists to be able to say no. "Said no" is only half the
// contract; the other half is that the command left NOTHING behind. Each
// test below fails one pre- hook and checks the invariant specific to the
// command -- the state that command would have mutated -- plus the shared
// invariant that the matching post- hook never fired.
//
// pre-worktree-switch is deliberately absent here:
// TestSwitchHooks_FailingPreLeavesSymlinkUntouched already covers it.

// assertNoPostHook is the invariant every abort shares: a pre- hook that
// vetoes must stop the operation before its post- counterpart. A post-
// that fires anyway means the veto did not actually cut the path.
func assertNoPostHook(t *testing.T, records []map[string]string, postHook string) {
	t.Helper()
	for _, rec := range records {
		if rec["hook"] == postHook {
			t.Errorf("%s fired despite the failing pre- hook: %+v", postHook, rec)
		}
	}
}

// TestLifecycleHooks_FailingPreCloneLeavesNothingOnDisk: pre-clone fires
// before any filesystem work, so a veto must leave no project root at all.
// This is the strictest of the abort invariants -- there is no partial
// clone to tolerate, only "the directory does not exist".
func TestLifecycleHooks_FailingPreCloneLeavesNothingOnDisk(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	seedRemote(t, env)
	installLifecycleHooks(t, env)
	writeLifecycleHook(t, env, "pre-clone", "1")

	_, stderr, code := env.RunCommandWithExit(t, env.RootDir, env.BinPath, env.BareRepoPath, "hub")
	if code == 0 {
		t.Fatalf("clone should have failed when pre-clone exits non-zero; stderr: %s", stderr)
	}
	t.Logf("blocked clone stderr:\n%s", stderr)

	records := readLifecycleRecords(t, env)
	assertHookSeq(t, records, []string{"pre-clone"})
	assertNoPostHook(t, records, "post-worktree-add")
	assertNoPostHook(t, records, "post-clone")

	if _, err := os.Stat(env.HubPath); !os.IsNotExist(err) {
		t.Errorf("hub %q exists despite the pre-clone veto (stat err = %v)", env.HubPath, err)
	}
	// The hopspace is the other half of a clone's footprint: a clone that
	// got far enough to allocate one has left state behind even if the
	// hub was cleaned up.
	if _, err := os.Stat(env.DataHome); !os.IsNotExist(err) {
		t.Errorf("hopspace data home %q exists despite the pre-clone veto (stat err = %v)",
			env.DataHome, err)
	}
}

// TestLifecycleHooks_FailingPreAddLeavesNoWorktreeOrBranch: a vetoed `add`
// must leave no worktree directory, no branch ref, and no hub entry.
//
// The branch and hub-config checks are the ones that matter beyond what
// TestGitFlowIntegration_BranchNameValidation already covers (it checks
// the directory alone): `add` creates a branch ref and registers it in
// hop.json, and either surviving a veto would leave `git hop list`
// advertising a worktree that is not there.
func TestLifecycleHooks_FailingPreAddLeavesNoWorktreeOrBranch(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	seedRemote(t, env)
	installLifecycleHooks(t, env)
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	resetLifecycleLog(t, env)

	writeLifecycleHook(t, env, "pre-worktree-add", "1")

	_, stderr, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "add", "blocked")
	if code == 0 {
		t.Fatalf("add should have failed when pre-worktree-add exits non-zero; stderr: %s", stderr)
	}
	t.Logf("blocked add stderr:\n%s", stderr)

	records := readLifecycleRecords(t, env)
	assertHookSeq(t, records, []string{"pre-worktree-add"})
	assertNoPostHook(t, records, "post-worktree-add")

	worktree := filepath.Join(env.HubPath, "hops", "blocked")
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Errorf("worktree %q exists despite the pre-worktree-add veto (stat err = %v)", worktree, err)
	}
	if branches := env.RunCommand(t, env.HubPath, "git", "branch", "--list", "blocked"); strings.TrimSpace(branches) != "" {
		t.Errorf("branch 'blocked' was created despite the veto: %q", branches)
	}
	assertHubConfigLacks(t, env, "blocked")
}

// TestLifecycleHooks_FailingPreMoveLeavesWorktreeAtOldPath: a vetoed
// `move` must leave the worktree exactly where it was -- old path present,
// new path absent, old branch intact. A half-applied rename is the worst
// outcome here: the hub would point at a path that no longer exists.
func TestLifecycleHooks_FailingPreMoveLeavesWorktreeAtOldPath(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	seedRemote(t, env)
	installLifecycleHooks(t, env)
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	env.RunGitHop(t, env.HubPath, "add", "feature-a")
	resetLifecycleLog(t, env)

	writeLifecycleHook(t, env, "pre-worktree-move", "1")

	_, stderr, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "move", "feature-a", "feature-b")
	if code == 0 {
		t.Fatalf("move should have failed when pre-worktree-move exits non-zero; stderr: %s", stderr)
	}
	t.Logf("blocked move stderr:\n%s", stderr)

	records := readLifecycleRecords(t, env)
	assertHookSeq(t, records, []string{"pre-worktree-move"})
	assertNoPostHook(t, records, "post-worktree-move")

	oldPath := filepath.Join(env.HubPath, "hops", "feature-a")
	newPath := filepath.Join(env.HubPath, "hops", "feature-b")
	if _, err := os.Stat(oldPath); err != nil {
		t.Errorf("worktree missing from old path %q after the veto: %v", oldPath, err)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Errorf("worktree appeared at new path %q despite the veto (stat err = %v)", newPath, err)
	}
	if branches := env.RunCommand(t, env.HubPath, "git", "branch", "--list", "feature-a"); strings.TrimSpace(branches) == "" {
		t.Error("branch feature-a was renamed away despite the pre-worktree-move veto")
	}
	assertHubConfigLacks(t, env, "feature-b")
}

// TestLifecycleHooks_FailingPreRemoveLeavesWorktreeIntact: a vetoed
// `remove` must leave the worktree, its branch, and its hub entry all in
// place. remove is the one command whose partial application is
// unrecoverable, so "nothing was deleted" is the whole contract.
func TestLifecycleHooks_FailingPreRemoveLeavesWorktreeIntact(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	seedRemote(t, env)
	installLifecycleHooks(t, env)
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	env.RunGitHop(t, env.HubPath, "add", "feature-a")
	resetLifecycleLog(t, env)

	writeLifecycleHook(t, env, "pre-worktree-remove", "1")

	_, stderr, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "remove", "feature-a", "--no-prompt")
	if code == 0 {
		t.Fatalf("remove should have failed when pre-worktree-remove exits non-zero; stderr: %s", stderr)
	}
	t.Logf("blocked remove stderr:\n%s", stderr)

	records := readLifecycleRecords(t, env)
	assertHookSeq(t, records, []string{"pre-worktree-remove"})
	assertNoPostHook(t, records, "post-worktree-remove")

	worktree := filepath.Join(env.HubPath, "hops", "feature-a")
	if _, err := os.Stat(worktree); err != nil {
		t.Errorf("worktree %q was removed despite the pre-worktree-remove veto: %v", worktree, err)
	}
	if branches := env.RunCommand(t, env.HubPath, "git", "branch", "--list", "feature-a"); strings.TrimSpace(branches) == "" {
		t.Error("branch feature-a was deleted despite the pre-worktree-remove veto")
	}
	assertHubConfigContains(t, env, "feature-a")
}

// readHubConfig returns the hub's hop.json as text. The assertions below
// only need to know whether a branch is registered, so substring matching
// on the raw JSON is enough and keeps this independent of the config
// struct's shape.
func readHubConfig(t *testing.T, env *TestEnv) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(env.HubPath, "hop.json"))
	if err != nil {
		t.Fatalf("failed to read hub hop.json: %v", err)
	}
	return string(data)
}

func assertHubConfigLacks(t *testing.T, env *TestEnv, branch string) {
	t.Helper()
	if cfg := readHubConfig(t, env); strings.Contains(cfg, `"`+branch+`"`) {
		t.Errorf("hub hop.json registered branch %q despite the veto:\n%s", branch, cfg)
	}
}

func assertHubConfigContains(t *testing.T, env *TestEnv, branch string) {
	t.Helper()
	if cfg := readHubConfig(t, env); !strings.Contains(cfg, `"`+branch+`"`) {
		t.Errorf("hub hop.json lost branch %q despite the veto:\n%s", branch, cfg)
	}
}
