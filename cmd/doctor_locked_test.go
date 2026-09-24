package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/state"
)

// A worktree git has locked (`git worktree lock`, e.g. one on a drive
// that is not mounted) whose directory is missing is not broken: git
// refuses to recreate or prune it, and the directory may come back. Doctor
// reports it as a warning, naming the lock reason and how to unlock it,
// and leaves it alone in every mode.

// lockedMissingHub sets up a hub whose usb worktree is missing and locked
// with lockLine ("locked" or "locked <reason>"), recorded in state too.
func lockedMissingHub(t *testing.T, lockLine string) (afero.Fs, *registryGit, string) {
	t.Helper()
	p := isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	// Healthy paths, so the only finding left is the locked worktree.
	require.NoError(t, fs.MkdirAll(p.dataHome, 0o755))
	require.NoError(t, fs.MkdirAll(filepath.Join(p.cacheHome, "git-hop"), 0o755))
	hubPath := "/hubs/repo"
	doctorHub(t, fs, hubPath, []string{"main", "usb"}, []string{"main"})
	usb := worktreeDir(hubPath, "usb")

	st := stateWithHub(hubPath)
	st.Repositories["github.com/test/repo"].Worktrees = map[string]*state.WorktreeState{
		"usb": {Path: usb, Type: "linked", HubPath: hubPath},
	}
	require.NoError(t, state.SaveState(fs, st))

	g := newRegistryGit(fs, usb)
	g.WorktreeListOut = porcelainEntry(usb, "usb", lockLine)
	return fs, g, hubPath
}

func TestDoctor_LockedMissingWorktree_IsAWarning(t *testing.T) {
	for _, opts := range []doctorOpts{{}, {fix: true, dryRun: true}, {fix: true}} {
		name := "check"
		switch {
		case opts.planning():
			name = "dry-run"
		case opts.fix:
			name = "fix"
		}
		t.Run(name, func(t *testing.T) {
			fs, g, hubPath := lockedMissingHub(t, "locked on the usb drive")
			usb := worktreeDir(hubPath, "usb")

			r := runDoctor(fs, g, hubPath, opts)

			assert.NoError(t, doctorResult(r), "a locked worktree is a warning, not an issue: %+v", r.records)
			for _, rec := range r.records {
				if rec.Subject == "usb" || rec.Subject == "github.com/test/repo:usb" || rec.Subject == usb {
					assert.Equal(t, doctorKindWarning, rec.Kind, "only warnings about the locked worktree: %+v", rec)
				}
			}
			warnings := strings.Join(recordMessages(r, doctorKindWarning, "usb"), "\n")
			assert.Contains(t, warnings, "locked")
			assert.Contains(t, warnings, "on the usb drive", "the warning names git's lock reason")
			assert.Contains(t, warnings, "git worktree unlock "+usb, "the warning says how to let doctor handle it")

			assert.Empty(t, g.calls, "nothing is recreated or unregistered")
			exists, _ := afero.DirExists(fs, usb)
			assert.False(t, exists, "no directory is created, not even on a preview")
			assert.Contains(t, hubBranchKeys(t, fs, hubPath), "usb", "the hop.json row is kept")
			st, err := state.LoadState(fs)
			require.NoError(t, err)
			assert.Contains(t, st.Repositories["github.com/test/repo"].Worktrees, "usb", "the state entry is kept")
		})
	}
}

// TestDoctor_LockedMissingWorktree_NoReason: a lock without a reason is
// still a warning, and the message does not invent one.
func TestDoctor_LockedMissingWorktree_NoReason(t *testing.T) {
	fs, g, hubPath := lockedMissingHub(t, "locked")

	r := runDoctor(fs, g, hubPath, doctorOpts{fix: true})

	assert.NoError(t, doctorResult(r), "%+v", r.records)
	warnings := recordMessages(r, doctorKindWarning, "usb")
	require.Len(t, warnings, 1)
	assert.NotContains(t, warnings[0], "()")
	assert.Contains(t, warnings[0], "git worktree unlock")
	assert.Empty(t, g.calls)
}

func TestWorktreeLock_Reason(t *testing.T) {
	p := "/hubs/repo/hops/usb"
	for _, tc := range []struct {
		porcelain   string
		reason      string
		locked      bool
		description string
	}{
		{porcelainEntry(p, "usb", "locked on the usb drive"), "on the usb drive", true, "reason"},
		{porcelainEntry(p, "usb", "locked"), "", true, "no reason"},
		{porcelainEntry(p, "usb", "prunable"), "", false, "prunable, not locked"},
		{porcelainEntry("/hubs/repo/hops/other", "other", "locked x"), "", false, "another path's lock"},
	} {
		reason, locked := worktreeLock(tc.porcelain, p)
		assert.Equal(t, tc.locked, locked, tc.description)
		assert.Equal(t, tc.reason, reason, tc.description)
	}
}
