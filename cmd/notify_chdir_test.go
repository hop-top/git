package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/hooks"
)

// exitCodeFor maps a runNotifyChdir return to the process status the shell
// handler will actually see.
//
// The cobra RunE raises the directive with os.Exit, which a unit test cannot
// observe, so the translation is mirrored here. Asserting on the status
// rather than on the sentinel keeps the tests pinned to the contract the
// handler depends on instead of to an internal representation.
func exitCodeFor(err error) int {
	if errors.Is(err, errNavigationHandled) {
		return hooks.ExitNavigationHandled
	}
	return 0
}

// notifyFixture is a hub with two registered worktrees and real hook
// scripts on disk that append their environment to a log.
//
// Real files, not a MemMapFs: the hook runner execs the scripts, and an
// in-memory filesystem has nothing for the OS to exec.
type notifyFixture struct {
	hubPath   string
	worktreeA string
	worktreeB string
	hookLog   string
}

func newNotifyFixture(t *testing.T) notifyFixture {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	fx := notifyFixture{
		hubPath:   filepath.Join(root, "hub"),
		worktreeA: filepath.Join(root, "hub", "hops", "alpha"),
		worktreeB: filepath.Join(root, "hub", "hops", "beta"),
		hookLog:   filepath.Join(root, "hooks.log"),
	}

	for _, d := range []string{fx.worktreeA, fx.worktreeB} {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}

	hub := map[string]any{
		"repo": map[string]any{
			"uri": "git@github.com:acme/widget.git", "org": "acme",
			"repo": "widget", "defaultBranch": "alpha",
		},
		"branches": map[string]any{
			"alpha": map[string]any{"path": filepath.Join("hops", "alpha"), "hopspaceBranch": "alpha"},
			"beta":  map[string]any{"path": filepath.Join("hops", "beta"), "hopspaceBranch": "beta"},
		},
	}
	data, err := json.Marshal(hub)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(fx.hubPath, "hop.json"), data, 0o644))

	return fx
}

// installHook writes an executable hook that records the switch-related
// environment it was handed, one line per invocation.
func (fx notifyFixture) installHook(t *testing.T, name string) {
	t.Helper()

	dir := filepath.Join(fx.hubPath, ".git-hop", "hooks")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	script := "#!/bin/sh\n" +
		"printf '%s trigger=%s from_branch=%s from_path=%s branch=%s wt=%s\\n' \\\n" +
		"  \"$GIT_HOP_HOOK_NAME\" \"$GIT_HOP_TRIGGER\" \"$GIT_HOP_FROM_BRANCH\" \\\n" +
		"  \"$GIT_HOP_FROM_WORKTREE_PATH\" \"$GIT_HOP_BRANCH\" \"$GIT_HOP_WORKTREE_PATH\" \\\n" +
		"  >> \"" + fx.hookLog + "\"\n" +
		"exit 0\n"

	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755))
}

func (fx notifyFixture) hookLines(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(fx.hookLog)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)

	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// TestNotifyChdir_FiresPostHookWithChdirTrigger is the core contract: a cd
// into a registered worktree dispatches post-worktree-switch, and the hook
// can tell it apart from a `git hop` switch by GIT_HOP_TRIGGER.
func TestNotifyChdir_FiresPostHookWithChdirTrigger(t *testing.T) {
	fx := newNotifyFixture(t)
	fx.installHook(t, "post-worktree-switch")

	require.NoError(t, runNotifyChdir(afero.NewOsFs(), fx.worktreeA, ""))

	lines := fx.hookLines(t)
	require.Len(t, lines, 1, "expected exactly one hook invocation")
	require.Contains(t, lines[0], "post-worktree-switch")
	require.Contains(t, lines[0], "trigger=chdir")
	require.Contains(t, lines[0], "branch=alpha")
	require.Contains(t, lines[0], "wt="+fx.worktreeA)
}

// TestNotifyChdir_NeverFiresPreHook pins the second hard requirement of
// this change.
//
// The cd has already happened by the time anything here runs; it cannot be
// vetoed. Firing pre-worktree-switch would hand a hook a veto that nothing
// honours, so the pre- hook must never run on this path -- asserted by
// installing ONLY the pre- hook and requiring silence.
func TestNotifyChdir_NeverFiresPreHook(t *testing.T) {
	fx := newNotifyFixture(t)
	fx.installHook(t, "pre-worktree-switch")

	require.NoError(t, runNotifyChdir(afero.NewOsFs(), fx.worktreeA, ""))

	require.Empty(t, fx.hookLines(t),
		"pre-worktree-switch ran on a chdir; the cd is not abortable so it must never fire")
}

// From-state on the chdir path comes from $OLDPWD, because a plain cd never
// updates the `current` symlink the hop path reads.
func TestNotifyChdir_FromStateComesFromOldPwd(t *testing.T) {
	fx := newNotifyFixture(t)
	fx.installHook(t, "post-worktree-switch")

	require.NoError(t, runNotifyChdir(afero.NewOsFs(), fx.worktreeB, fx.worktreeA))

	lines := fx.hookLines(t)
	require.Len(t, lines, 1)
	require.Contains(t, lines[0], "from_branch=alpha")
	require.Contains(t, lines[0], "from_path="+fx.worktreeA)
	require.Contains(t, lines[0], "branch=beta")
}

// A previous directory that was not itself a registered worktree leaves the
// from-fields unset, matching how the hop path reports a first switch.
func TestNotifyChdir_UnregisteredOldPwdLeavesFromStateEmpty(t *testing.T) {
	fx := newNotifyFixture(t)
	fx.installHook(t, "post-worktree-switch")

	require.NoError(t, runNotifyChdir(afero.NewOsFs(), fx.worktreeA, filepath.Dir(fx.hubPath)))

	lines := fx.hookLines(t)
	require.Len(t, lines, 1)
	require.Contains(t, lines[0], "from_branch= ")
	require.Contains(t, lines[0], "from_path= ")
}

// Moving between subdirectories of one worktree is not a switch, so the
// binary must stay silent even when the shell decided to ask.
func TestNotifyChdir_SameWorktreeIsNotASwitch(t *testing.T) {
	fx := newNotifyFixture(t)
	fx.installHook(t, "post-worktree-switch")

	sub := filepath.Join(fx.worktreeA, "internal")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	require.NoError(t, runNotifyChdir(afero.NewOsFs(), sub, fx.worktreeA))

	require.Empty(t, fx.hookLines(t), "movement inside one worktree must not announce a switch")
}

// The shell's cache is fast, not authoritative -- it can name a worktree
// that has since been removed. The binary re-derives everything from
// hop.json, and a path that no longer resolves is a stale cache rather than
// an error.
func TestNotifyChdir_UnregisteredPathIsQuietlyIgnored(t *testing.T) {
	fx := newNotifyFixture(t)
	fx.installHook(t, "post-worktree-switch")

	stale := filepath.Join(fx.hubPath, "hops", "gone")
	require.NoError(t, os.MkdirAll(stale, 0o755))

	require.NoError(t, runNotifyChdir(afero.NewOsFs(), stale, ""))
	require.Empty(t, fx.hookLines(t))
}

// A path outside any hub must not error either: the handler can fire on a
// directory whose hub was deleted between cache write and cd.
func TestNotifyChdir_PathOutsideHubIsQuietlyIgnored(t *testing.T) {
	fx := newNotifyFixture(t)
	fx.installHook(t, "post-worktree-switch")

	outside, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	require.NoError(t, runNotifyChdir(afero.NewOsFs(), outside, ""))
	require.Empty(t, fx.hookLines(t))
}

// TestNotifyChdirCommand_IsHidden keeps the subcommand plumbing-only. It is
// invoked by the installed integration; a user typing it would be
// announcing a switch that did not happen.
func TestNotifyChdirCommand_IsHidden(t *testing.T) {
	require.True(t, notifyChdirCmd.Hidden, "__notify-chdir must stay hidden")
}

// installExitingHook writes a post-worktree-switch hook that records its
// invocation and then exits with the given status, standing in for a hook
// that navigated the user itself.
func (fx notifyFixture) installExitingHook(t *testing.T, code int) {
	t.Helper()

	dir := filepath.Join(fx.hubPath, ".git-hop", "hooks")
	require.NoError(t, os.MkdirAll(dir, 0o755))

	script := "#!/bin/sh\n" +
		"printf 'ran\\n' >> \"" + fx.hookLog + "\"\n" +
		"exit " + strconv.Itoa(code) + "\n"

	require.NoError(t, os.WriteFile(filepath.Join(dir, "post-worktree-switch"), []byte(script), 0o755))
}

// TestNotifyChdir_NavigationHandledPropagates is the regression test for the
// contamination this fix exists to stop.
//
// The directive used to be swallowed here on the reasoning that nothing on
// this path is waiting to cd. That reading is wrong in one direction: the
// user's own cd already happened, and a hook that navigates leaves the
// ORIGINATING shell sitting in the destination alongside the window the hook
// selected. The shell handler is what can undo that, and its only channel
// back from this process is the exit status -- so the directive has to
// survive the trip.
func TestNotifyChdir_NavigationHandledPropagates(t *testing.T) {
	fx := newNotifyFixture(t)
	fx.installExitingHook(t, hooks.ExitNavigationHandled)

	err := runNotifyChdir(afero.NewOsFs(), fx.worktreeB, fx.worktreeA)

	require.Len(t, fx.hookLines(t), 1, "the hook must still run")
	require.Equal(t, hooks.ExitNavigationHandled, exitCodeFor(err),
		"a navigating hook's directive must reach the shell handler")
}

// A hook that finishes normally reports nothing, so the user's cd stands.
func TestNotifyChdir_PlainSuccessExitsZero(t *testing.T) {
	fx := newNotifyFixture(t)
	fx.installExitingHook(t, 0)

	err := runNotifyChdir(afero.NewOsFs(), fx.worktreeB, fx.worktreeA)

	require.Len(t, fx.hookLines(t), 1)
	require.Equal(t, 0, exitCodeFor(err))
}

// A FAILING hook must not read as the directive either. The cd already
// happened and cannot be undone by anyone; turning a broken hook into a
// restore would move the user somewhere they did not ask to be, on top of
// the failure they already have.
func TestNotifyChdir_FailingHookDoesNotRestore(t *testing.T) {
	fx := newNotifyFixture(t)
	fx.installExitingHook(t, 1)

	err := runNotifyChdir(afero.NewOsFs(), fx.worktreeB, fx.worktreeA)

	require.Len(t, fx.hookLines(t), 1)
	require.Equal(t, 0, exitCodeFor(err),
		"a failing hook must leave the user's cd alone")
}

// The quiet paths -- stale cache, no hub, movement inside one worktree --
// stay quiet AND stay at exit 0. Nothing navigated, so nothing is undone.
func TestNotifyChdir_QuietPathsExitZero(t *testing.T) {
	fx := newNotifyFixture(t)
	fx.installExitingHook(t, hooks.ExitNavigationHandled)

	stale := filepath.Join(fx.hubPath, "hops", "gone")
	require.NoError(t, os.MkdirAll(stale, 0o755))
	sub := filepath.Join(fx.worktreeA, "internal")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	outside, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	for name, args := range map[string][2]string{
		"stale cache entry":    {stale, ""},
		"outside any hub":      {outside, ""},
		"same worktree in/out": {sub, fx.worktreeA},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, 0, exitCodeFor(runNotifyChdir(afero.NewOsFs(), args[0], args[1])))
		})
	}

	require.Empty(t, fx.hookLines(t), "none of these are switches, so no hook may run")
}
