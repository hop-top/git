package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

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
