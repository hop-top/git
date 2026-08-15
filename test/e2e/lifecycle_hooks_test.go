package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Coherence of the lifecycle hook surface, end to end.
//
// Every dispatch site already has a test proving it calls the runner. What
// those cannot show is that the sites compose: that one continuous session
// -- clone, add, switch, move, remove -- fires the right hooks in the right
// ORDER with the right env. Ordering is the property that regresses
// silently, and a set-membership assertion would stay green through a
// swapped pre-/post- pair or a dispatch moved across the operation it is
// supposed to bracket. So every assertion here is on the exact sequence.
//
// The recording hook is installed under EVERY lifecycle name at the GLOBAL
// level ($XDG_CONFIG_HOME/git-hop/hooks). Global is the only tier that
// exists before `clone` runs: pre-clone fires before any repo, hub, or
// hopspace is on disk, so a repo- or hopspace-level hook could not be
// reached by it. Installing one set of names in one place also means the
// log records what git-hop CHOSE to fire, not what the test arranged to be
// findable per command.

// recordLifecycleHook appends one line per invocation to a shared log.
//
// Presence is spelled with ${VAR+set} rather than "$VAR", because the two
// differ in exactly the case that matters: SwitchEnvVars OMITS the
// GIT_HOP_FROM_* keys when there is no previous worktree, and a hook must
// be able to tell "no previous worktree" from "previous worktree was the
// empty string". Reading "$VAR" collapses both to "", which is precisely
// the distinction the omission exists to preserve.
//
// worktree_dir records whether GIT_HOP_WORKTREE_PATH exists AT THE MOMENT
// THE HOOK RUNS. That is the only observable that pins a hook's position
// relative to the work its command does, as opposed to relative to the
// other hook: hoisting post-worktree-remove to fire straight after the
// pre- hook leaves the hook NAMES in the same order, so no sequence
// assertion can see it -- but the worktree is still on disk at that
// point, and a post- hook that reports "present" has been moved before
// the teardown it is supposed to follow.
const recordLifecycleHook = `#!/bin/bash
MARKER_DIR="$GIT_HOP_TEST_MARKER_DIR"
mkdir -p "$MARKER_DIR"
{
  echo "hook=$GIT_HOP_HOOK_NAME"
  echo "branch=$GIT_HOP_BRANCH"
  echo "worktree=$GIT_HOP_WORKTREE_PATH"
  echo "from_branch=$GIT_HOP_FROM_BRANCH"
  echo "from_branch_present=${GIT_HOP_FROM_BRANCH+set}"
  echo "from_worktree=$GIT_HOP_FROM_WORKTREE_PATH"
  echo "from_worktree_present=${GIT_HOP_FROM_WORKTREE_PATH+set}"
  echo "trigger=$GIT_HOP_TRIGGER"
  if [ -d "$GIT_HOP_WORKTREE_PATH" ]; then
    echo "worktree_dir=present"
  else
    echo "worktree_dir=absent"
  fi
  echo "---"
} >> "$MARKER_DIR/lifecycle.log"
exit %s
`

// lifecycleHookNames is every hook the clone→add→switch→move→remove
// lifecycle is expected to reach. Installing the recorder under all of
// them at once is what makes the sequence assertions meaningful: an
// unexpected extra dispatch shows up as an extra line rather than being
// silently unobservable.
var lifecycleHookNames = []string{
	"pre-clone",
	"post-clone",
	"pre-worktree-add",
	"post-worktree-add",
	"pre-worktree-switch",
	"post-worktree-switch",
	"pre-worktree-move",
	"post-worktree-move",
	"pre-worktree-remove",
	"post-worktree-remove",
}

// globalHooksDir is the hook tier that is reachable before a clone exists.
func globalHooksDir(env *TestEnv) string {
	return filepath.Join(env.RootDir, ".config", "git-hop", "hooks")
}

// installLifecycleHooks writes the recorder under every lifecycle hook
// name, all exiting 0.
func installLifecycleHooks(t *testing.T, env *TestEnv) {
	t.Helper()
	for _, name := range lifecycleHookNames {
		writeLifecycleHook(t, env, name, "0")
	}
}

// writeLifecycleHook installs (or replaces) the recorder under one hook
// name with the given exit code.
func writeLifecycleHook(t *testing.T, env *TestEnv, hookName, exitCode string) {
	t.Helper()
	dir := globalHooksDir(env)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create global hooks dir: %v", err)
	}
	path := filepath.Join(dir, hookName)
	WriteFile(t, path, strings.Replace(recordLifecycleHook, "%s", exitCode, 1))
	if err := os.Chmod(path, 0755); err != nil {
		t.Fatalf("failed to make %s executable: %v", hookName, err)
	}
}

// seedRemote builds the bare origin the lifecycle clones from. Stops short
// of cloning: these tests drive `clone` themselves, since the clone hooks
// are half of what is under test.
func seedRemote(t *testing.T, env *TestEnv) {
	t.Helper()
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")
}

// readLifecycleRecords splits the log into one map per invocation, in fire
// order. A missing log means no hook ever ran, which callers assert on
// directly, so it reads back as empty rather than fatal.
func readLifecycleRecords(t *testing.T, env *TestEnv) []map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(env.RootDir, "markers", "lifecycle.log"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("failed to read lifecycle hook log: %v", err)
	}
	var records []map[string]string
	cur := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "---" {
			records = append(records, cur)
			cur = map[string]string{}
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			cur[k] = v
		}
	}
	return records
}

// resetLifecycleLog truncates the log between phases so each phase's
// sequence assertion stands alone. The whole-run sequence is asserted
// separately by TestLifecycleHooks_FullSequence.
func resetLifecycleLog(t *testing.T, env *TestEnv) {
	t.Helper()
	if err := os.Remove(filepath.Join(env.RootDir, "markers", "lifecycle.log")); err != nil && !os.IsNotExist(err) {
		t.Fatalf("failed to reset lifecycle log: %v", err)
	}
}

// hookSeq projects the records down to just the fire order.
func hookSeq(records []map[string]string) []string {
	seq := make([]string, 0, len(records))
	for _, rec := range records {
		seq = append(seq, rec["hook"])
	}
	return seq
}

// assertHookSeq compares the observed fire order against want, EXACTLY --
// same hooks, same order, same count.
func assertHookSeq(t *testing.T, records []map[string]string, want []string) {
	t.Helper()
	got := hookSeq(records)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("hook sequence = %v; want %v", got, want)
	}
}

// TestLifecycleHooks_FullSequence drives one continuous session through
// every lifecycle command and pins the sequence of the whole run.
//
// Per-command sequences are asserted phase by phase below; this asserts
// the concatenation, which is what catches a dispatch that leaks across
// the boundary of the command that owns it -- e.g. an `add` that also
// fires switch hooks because it moves `current`, or a `remove` whose
// symlink fixup re-enters the switch path.
func TestLifecycleHooks_FullSequence(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	seedRemote(t, env)
	installLifecycleHooks(t, env)

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	env.RunGitHop(t, env.HubPath, "add", "feature-a")
	env.RunGitHop(t, env.HubPath, "main")
	env.RunGitHop(t, env.HubPath, "move", "feature-a", "feature-b")
	env.RunGitHop(t, env.HubPath, "remove", "feature-b", "--no-prompt")

	assertHookSeq(t, readLifecycleRecords(t, env), []string{
		// clone: pre-clone brackets the whole operation; the initial
		// worktree gets its own post-worktree-add; post-clone lands last,
		// after that worktree is fully registered.
		"pre-clone", "post-worktree-add", "post-clone",
		"pre-worktree-add", "post-worktree-add",
		"pre-worktree-switch", "post-worktree-switch",
		"pre-worktree-move", "post-worktree-move",
		"pre-worktree-remove", "post-worktree-remove",
	})
}

// TestLifecycleHooks_PerCommandSequences asserts each command's own
// bracket in isolation, along with the branch each hook was handed.
//
// Split from the full-sequence test so a regression names the command it
// broke rather than just reporting a long diff, and so per-command env can
// be checked at the point the command runs.
func TestLifecycleHooks_PerCommandSequences(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	seedRemote(t, env)
	installLifecycleHooks(t, env)

	t.Run("clone", func(t *testing.T) {
		// Reset on the way out even when an assertion fatals, so a
		// failing phase does not hand the next one a dirty log and
		// turn one regression into five.
		t.Cleanup(func() { resetLifecycleLog(t, env) })
		env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
		records := readLifecycleRecords(t, env)
		assertHookSeq(t, records, []string{
			"pre-clone", "post-worktree-add", "post-clone",
		})

		// pre-clone runs before the remote has been consulted, so the
		// branch genuinely is not known yet. A guessed value here would
		// be worse than none.
		if got := records[0]["branch"]; got != "" {
			t.Errorf("pre-clone branch = %q; want empty", got)
		}

		// post-worktree-add and post-clone both describe the INITIAL
		// WORKTREE, not the hub root. A hook handed the hub root would be
		// pointed at a bare repo with no source tree.
		wantWorktree := evalPath(t, filepath.Join(env.HubPath, "hops", "main"))
		for _, rec := range records[1:] {
			if rec["branch"] != "main" {
				t.Errorf("%s: branch = %q; want main", rec["hook"], rec["branch"])
			}
			if got := evalPath(t, rec["worktree"]); got != wantWorktree {
				t.Errorf("%s: worktree = %q; want initial worktree %q",
					rec["hook"], got, wantWorktree)
			}
		}
	})

	t.Run("add", func(t *testing.T) {
		// Reset on the way out even when an assertion fatals, so a
		// failing phase does not hand the next one a dirty log and
		// turn one regression into five.
		t.Cleanup(func() { resetLifecycleLog(t, env) })
		env.RunGitHop(t, env.HubPath, "add", "feature-a")
		records := readLifecycleRecords(t, env)
		assertHookSeq(t, records, []string{"pre-worktree-add", "post-worktree-add"})

		// Resolve the hops dir and join the leaf: pre-worktree-add is
		// handed a path that does not exist yet, so resolving the full
		// path would leave it unresolved while the recorded value is
		// resolved.
		wantWorktree := filepath.Join(evalPath(t, filepath.Join(env.HubPath, "hops")), "feature-a")
		for _, rec := range records {
			if rec["branch"] != "feature-a" {
				t.Errorf("%s: branch = %q; want feature-a", rec["hook"], rec["branch"])
			}
			// pre-worktree-add is handed the path the worktree WILL take,
			// before it exists -- that is what lets a hook veto by path.
			if got := evalPath(t, rec["worktree"]); got != wantWorktree {
				t.Errorf("%s: worktree = %q; want %q", rec["hook"], got, wantWorktree)
			}
		}

		// Same bracket-the-work check as remove, mirrored: add's pre- hook
		// runs before the worktree exists (so it can veto by path) and its
		// post- hook runs after (so it can populate the tree). Sequence
		// alone would stay green if BOTH dispatches moved to one side.
		pre, post := records[0], records[1]
		if pre["worktree_dir"] != "absent" {
			t.Errorf("pre-worktree-add saw worktree_dir=%q; want absent -- "+
				"the dispatch has slipped past the creation it must precede",
				pre["worktree_dir"])
		}
		if post["worktree_dir"] != "present" {
			t.Errorf("post-worktree-add saw worktree_dir=%q; want present -- "+
				"a post- hook must run once the worktree exists",
				post["worktree_dir"])
		}
	})

	t.Run("switch", func(t *testing.T) {
		// Reset on the way out even when an assertion fatals, so a
		// failing phase does not hand the next one a dirty log and
		// turn one regression into five.
		t.Cleanup(func() { resetLifecycleLog(t, env) })
		env.RunGitHop(t, env.HubPath, "main")
		records := readLifecycleRecords(t, env)
		assertHookSeq(t, records, []string{"pre-worktree-switch", "post-worktree-switch"})

		for _, rec := range records {
			if rec["branch"] != "main" {
				t.Errorf("%s: branch = %q; want main", rec["hook"], rec["branch"])
			}
			// The trigger distinguishes an explicit hop from the shell
			// integration's chdir-driven dispatch; both reach the same
			// hook name, so this is the only thing that separates them.
			if rec["trigger"] != "hop" {
				t.Errorf("%s: trigger = %q; want hop", rec["hook"], rec["trigger"])
			}
		}
	})

	t.Run("move", func(t *testing.T) {
		// Reset on the way out even when an assertion fatals, so a
		// failing phase does not hand the next one a dirty log and
		// turn one regression into five.
		t.Cleanup(func() { resetLifecycleLog(t, env) })
		env.RunGitHop(t, env.HubPath, "move", "feature-a", "feature-b")
		records := readLifecycleRecords(t, env)
		assertHookSeq(t, records, []string{"pre-worktree-move", "post-worktree-move"})

		// The move hooks bracket the rename, so each side describes its
		// OWN side of it: pre- sees the branch/path as they still are,
		// post- sees them as they now are. Collapsing both onto one side
		// would leave a hook unable to find the tree it must act on.
		pre, post := records[0], records[1]
		if pre["branch"] != "feature-a" {
			t.Errorf("pre-worktree-move branch = %q; want feature-a (pre-rename)", pre["branch"])
		}
		if post["branch"] != "feature-b" {
			t.Errorf("post-worktree-move branch = %q; want feature-b (post-rename)", post["branch"])
		}
		// Resolve the HOPS DIR and join the leaf, rather than resolving
		// each full path: by now the move has happened, so hops/feature-a
		// no longer exists and EvalSymlinks would hand back the
		// unresolved path while the recorded value is resolved -- a
		// spurious mismatch that says nothing about the hook.
		hops := evalPath(t, filepath.Join(env.HubPath, "hops"))
		wantOld := filepath.Join(hops, "feature-a")
		wantNew := filepath.Join(hops, "feature-b")
		if got := evalPath(t, pre["worktree"]); got != wantOld {
			t.Errorf("pre-worktree-move worktree = %q; want old path %q", got, wantOld)
		}
		if got := evalPath(t, post["worktree"]); got != wantNew {
			t.Errorf("post-worktree-move worktree = %q; want new path %q", got, wantNew)
		}
		// Each side must see ITS OWN path live: the rename happens
		// between them, so both report present, but for different
		// directories. A dispatch on the wrong side of the rename reports
		// absent, because the path it was handed does not exist yet (or
		// any more).
		if pre["worktree_dir"] != "present" {
			t.Errorf("pre-worktree-move saw worktree_dir=%q at the old path; want present",
				pre["worktree_dir"])
		}
		if post["worktree_dir"] != "present" {
			t.Errorf("post-worktree-move saw worktree_dir=%q at the new path; want present",
				post["worktree_dir"])
		}
	})

	t.Run("remove", func(t *testing.T) {
		// Reset on the way out even when an assertion fatals, so a
		// failing phase does not hand the next one a dirty log and
		// turn one regression into five.
		t.Cleanup(func() { resetLifecycleLog(t, env) })
		env.RunGitHop(t, env.HubPath, "remove", "feature-b", "--no-prompt")
		records := readLifecycleRecords(t, env)
		assertHookSeq(t, records, []string{"pre-worktree-remove", "post-worktree-remove"})

		for _, rec := range records {
			if rec["branch"] != "feature-b" {
				t.Errorf("%s: branch = %q; want feature-b", rec["hook"], rec["branch"])
			}
		}

		// The hooks must bracket the TEARDOWN, not merely each other.
		// pre- still sees the worktree (that is what makes a veto or a
		// last-look backup possible); post- must not (that is what makes
		// it a post-). Hook order alone cannot distinguish this: hoisting
		// the post- dispatch to just after the pre- one keeps the names in
		// sequence while firing it before anything was removed.
		pre, post := records[0], records[1]
		if pre["worktree_dir"] != "present" {
			t.Errorf("pre-worktree-remove saw worktree_dir=%q; want present -- "+
				"a pre- hook must run while the worktree is still there",
				pre["worktree_dir"])
		}
		if post["worktree_dir"] != "absent" {
			t.Errorf("post-worktree-remove saw worktree_dir=%q; want absent -- "+
				"the dispatch has moved ahead of the teardown it must follow",
				post["worktree_dir"])
		}
	})
}

// TestLifecycleHooks_SwitchFromStateAbsentOnFirstHop covers the
// distinction SwitchEnvVars exists to make.
//
// After a clone with no prior hop there is no previous worktree, so the
// GIT_HOP_FROM_* keys must be ABSENT from the hook's environment rather
// than present-and-empty. A hook that branches on `${GIT_HOP_FROM_BRANCH+set}`
// -- the natural way to ask "did I come from somewhere?" -- reads the two
// cases differently, and only absence answers correctly.
//
// TestSwitchHooks_NoCurrentSymlinkOmitsFromState already checks these read
// back as empty strings; that assertion cannot tell absent from set-empty,
// which is exactly the property being claimed. This closes that gap, and
// does it from the real first-hop-after-clone shape rather than by
// deleting the symlink after the fact.
func TestLifecycleHooks_SwitchFromStateAbsentOnFirstHop(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	env := SetupTestEnv(t)
	seedRemote(t, env)
	installLifecycleHooks(t, env)

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	env.RunGitHop(t, env.HubPath, "add", "feature-a")

	// `add` leaves `current` pointing at the branch it just created.
	// Dropping it reproduces the fresh-clone shape: a hub that has never
	// been hopped in.
	if err := os.Remove(filepath.Join(env.HubPath, "current")); err != nil {
		t.Fatalf("failed to remove current symlink: %v", err)
	}
	resetLifecycleLog(t, env)

	env.RunGitHop(t, env.HubPath, "feature-a")

	records := readLifecycleRecords(t, env)
	assertHookSeq(t, records, []string{"pre-worktree-switch", "post-worktree-switch"})

	for _, rec := range records {
		if rec["from_branch_present"] != "" {
			t.Errorf("%s: GIT_HOP_FROM_BRANCH is present (=%q); want absent from the environment",
				rec["hook"], rec["from_branch"])
		}
		if rec["from_worktree_present"] != "" {
			t.Errorf("%s: GIT_HOP_FROM_WORKTREE_PATH is present (=%q); want absent from the environment",
				rec["hook"], rec["from_worktree"])
		}
		// Trigger is unconditional, so its presence here is what shows
		// the absence above is selective omission and not the whole
		// switch env having failed to reach the hook.
		if rec["trigger"] != "hop" {
			t.Errorf("%s: trigger = %q; want hop", rec["hook"], rec["trigger"])
		}
	}
}

// TestLifecycleHooks_SwitchFromStatePresentOnSecondHop is the positive
// counterpart: once there IS a previous worktree, both keys are present
// and carry it. Without this, the absence assertion above would also pass
// against a build that had stopped exporting the keys entirely.
func TestLifecycleHooks_SwitchFromStatePresentOnSecondHop(t *testing.T) {
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

	// `add` left `current` on feature-a, so this hop is feature-a → main.
	env.RunGitHop(t, env.HubPath, "main")

	records := readLifecycleRecords(t, env)
	assertHookSeq(t, records, []string{"pre-worktree-switch", "post-worktree-switch"})

	wantFrom := evalPath(t, filepath.Join(env.HubPath, "hops", "feature-a"))
	for _, rec := range records {
		if rec["from_branch_present"] != "set" {
			t.Errorf("%s: GIT_HOP_FROM_BRANCH absent; want present", rec["hook"])
		}
		if rec["from_branch"] != "feature-a" {
			t.Errorf("%s: from_branch = %q; want feature-a", rec["hook"], rec["from_branch"])
		}
		if rec["from_worktree_present"] != "set" {
			t.Errorf("%s: GIT_HOP_FROM_WORKTREE_PATH absent; want present", rec["hook"])
		}
		if got := evalPath(t, rec["from_worktree"]); got != wantFrom {
			t.Errorf("%s: from_worktree = %q; want %q", rec["hook"], got, wantFrom)
		}
	}
}
