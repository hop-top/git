//go:build !windows

package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/hooks"
)

// A hook mirror writes into the hopspace hooks dir under the hopspace's
// hop.json lock, the lock doctor --fix moves that dir under. A move that
// starts while the mirror writes waits for it and carries the new hook;
// nothing is left at the old path.
//
// The mirror is held mid-write by a committed hook that is a FIFO: copy
// mode reads it under the lock, and the read blocks until the test
// writes the hook's content. Opening the FIFO's write end returns once
// the mirror has opened it, so the lock is held from then on.
func TestDoctorFix_LegacyHooksMoveWaitsForHookMirror(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	t.Setenv("GIT_HOP_DATA_HOME", data)
	gitconfig := filepath.Join(root, "gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", gitconfig)
	// The mirror resolves its hooks dir to the old location.
	out, err := exec.Command("git", "config", "--global", "hop.dataLayout", "{host}/{org}/{repo}").CombinedOutput()
	require.NoError(t, err, "%s", out)

	fs := afero.NewOsFs()
	legacy := filepath.Join(data, "github.com", "acme", "widgets", "hooks")
	newDir := filepath.Join(data, "acme", "widgets", "hooks")
	writeTestFile(t, fs, filepath.Join(legacy, "pre-worktree-add"), "#!/bin/sh\necho old\n")

	wt := filepath.Join(root, "wt")
	fifo := filepath.Join(wt, ".git-hop", "hooks", "post-worktree-add")
	require.NoError(t, os.MkdirAll(filepath.Dir(fifo), 0o755))
	require.NoError(t, syscall.Mkfifo(fifo, 0o755))

	mirrorDone := make(chan error, 1)
	go func() {
		res, err := hooks.MirrorCommittedHooks(fs, hooks.MirrorOpts{
			WorktreePath: wt,
			RepoID:       "github.com/acme/widgets",
			RepoURI:      "https://github.com/acme/widgets.git",
			Mode:         hooks.ModeCopy,
		})
		if err == nil && res.Installed != 1 {
			t.Errorf("mirror installed %d hooks, want 1 (%+v)", res.Installed, res.Hooks)
		}
		mirrorDone <- err
	}()

	opened := make(chan *os.File, 1)
	go func() {
		w, err := os.OpenFile(fifo, os.O_WRONLY, 0)
		if err != nil {
			t.Errorf("open fifo: %v", err)
			w = nil
		}
		opened <- w
	}()
	var w *os.File
	select {
	case w = <-opened:
		require.NotNil(t, w)
	case <-time.After(10 * time.Second):
		t.Fatal("the mirror never started writing the hook")
	}

	var r doctorReport
	move := startMove(func() error {
		reportLegacyHooksDir(fs, doctorOpts{fix: true}, &r, legacy, newDir)
		return nil
	})
	assertWaiting(t, move)
	_, err = w.WriteString("#!/bin/sh\necho new\n")
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NoError(t, recv(t, mirrorDone, "mirror"))
	require.NoError(t, recv(t, move, "move"))

	assert.Empty(t, r.failedRecords(), "the move must succeed once the mirror is done")
	gone, _ := afero.Exists(fs, legacy)
	assert.False(t, gone, "no hook may be left, or land, at the old path")
	body, err := afero.ReadFile(fs, filepath.Join(newDir, "post-worktree-add"))
	require.NoError(t, err, "the mirrored hook must move with the dir")
	assert.Equal(t, "#!/bin/sh\necho new\n", string(body))
	ok, _ := afero.Exists(fs, filepath.Join(newDir, "pre-worktree-add"))
	assert.True(t, ok, "the hooks the dir held must move too")
	assertNoLockFiles(t, fs, filepath.Dir(legacy), filepath.Dir(newDir))
}
