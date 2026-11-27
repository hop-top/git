package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

// Where the default branch lives in a removeDefaultHub.
const (
	defaultLocal   = "local"  // refs/heads/main
	defaultOrigin  = "origin" // refs/remotes/origin/main only
	defaultNowhere = "none"   // neither
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return string(out)
}

// removeDefaultHub builds a real bare hub whose default branch, main, has
// no worktree, with a worktree for feat that 'current' points at. where
// says where main still exists.
func removeDefaultHub(t *testing.T, where string) (fs afero.Fs, hub *hop.Hub, hubPath, featPath string) {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	require.NoError(t, os.MkdirAll(src, 0o755))
	gitIn(t, src, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(src, "a"), []byte("a"), 0o644))
	gitIn(t, src, "add", "a")
	gitIn(t, src, "commit", "-q", "-m", "init")
	gitIn(t, src, "branch", "feat")

	hubPath = filepath.Join(root, "hub")
	gitIn(t, root, "clone", "-q", "--bare", src, hubPath)
	switch where {
	case defaultOrigin:
		gitIn(t, hubPath, "update-ref", "refs/remotes/origin/main", "refs/heads/main")
		gitIn(t, hubPath, "update-ref", "-d", "refs/heads/main")
	case defaultNowhere:
		gitIn(t, hubPath, "update-ref", "-d", "refs/heads/main")
	}

	fs = afero.NewOsFs()
	var err error
	hub, err = hop.CreateHub(fs, hubPath, src, "o", "r", "main")
	require.NoError(t, err)
	featPath = filepath.Join(hubPath, "hops", "feat")
	gitIn(t, hubPath, "worktree", "add", "-q", featPath, "feat")
	require.NoError(t, hub.AddBranch("feat", "feat", featPath))
	require.NoError(t, hop.UpdateCurrentSymlink(fs, hubPath, featPath))
	return fs, hub, hubPath, featPath
}

// captureStreams runs f and returns what it wrote to stdout and stderr.
func captureStreams(t *testing.T, f func()) (stdout, stderr string) {
	t.Helper()
	or, ow, err := os.Pipe()
	require.NoError(t, err)
	er, ew, err := os.Pipe()
	require.NoError(t, err)
	oldO, oldE := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = ow, ew
	var ob, eb bytes.Buffer
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(&ob, or); done <- struct{}{} }()
	go func() { _, _ = io.Copy(&eb, er); done <- struct{}{} }()
	func() {
		defer func() { os.Stdout, os.Stderr = oldO, oldE }()
		f()
	}()
	ow.Close()
	ew.Close()
	<-done
	<-done
	return ob.String(), eb.String()
}

func currentLink(t *testing.T, hubPath string) (string, bool) {
	t.Helper()
	target, err := os.Readlink(filepath.Join(hubPath, "current"))
	if err != nil {
		require.True(t, os.IsNotExist(err), "unexpected: %v", err)
		return "", false
	}
	return target, true
}

// (a) the default branch exists locally; current named the removed worktree.
func TestRemove_CreatesDefaultWorktreeFromLocalBranch(t *testing.T) {
	fs, hub, hubPath, _ := removeDefaultHub(t, defaultLocal)

	_, err := removeBranchWorktreeOpts(fs, git.New(), hub, hubPath, "feat", removeOptions{})
	require.NoError(t, err)

	mainPath := filepath.Join(hubPath, "hops", "main")
	assert.DirExists(t, mainPath)
	assert.Contains(t, hub.Config.Branches, "main", "registered in hub")
	reloaded, err := hop.LoadHub(fs, hubPath)
	require.NoError(t, err)
	assert.Contains(t, reloaded.Config.Branches, "main", "registered in hop.json")
	assert.NotContains(t, reloaded.Config.Branches, "feat")
	target, ok := currentLink(t, hubPath)
	require.True(t, ok, "current must exist")
	assert.Equal(t, "hops/main", target)
	assert.Equal(t, "main\n", gitIn(t, mainPath, "branch", "--show-current"))
}

// (b) the default branch exists only as origin/main.
func TestRemove_CreatesDefaultWorktreeFromOriginBranch(t *testing.T) {
	fs, hub, hubPath, _ := removeDefaultHub(t, defaultOrigin)

	_, err := removeBranchWorktreeOpts(fs, git.New(), hub, hubPath, "feat", removeOptions{})
	require.NoError(t, err)

	mainPath := filepath.Join(hubPath, "hops", "main")
	assert.DirExists(t, mainPath)
	assert.Contains(t, hub.Config.Branches, "main")
	target, ok := currentLink(t, hubPath)
	require.True(t, ok)
	assert.Equal(t, "hops/main", target)
}

// (c) the default branch exists nowhere: removal succeeds, current is
// dropped, the reason is said, nothing is created.
func TestRemove_DefaultBranchNowhere_DropsCurrentWithWarning(t *testing.T) {
	fs, hub, hubPath, _ := removeDefaultHub(t, defaultNowhere)

	var err error
	_, stderr := captureStreams(t, func() {
		_, err = removeBranchWorktreeOpts(fs, git.New(), hub, hubPath, "feat", removeOptions{})
	})
	require.NoError(t, err)

	_, ok := currentLink(t, hubPath)
	assert.False(t, ok, "current must not be left dangling")
	assert.NoDirExists(t, filepath.Join(hubPath, "hops", "main"))
	assert.NotContains(t, hub.Config.Branches, "main")
	assert.Contains(t, stderr, "warning:")
	assert.Contains(t, stderr, "main")
}

// (c) any creator failure falls back the same way.
func TestUpdateCurrentToDefault_CreatorFailureDropsDanglingCurrent(t *testing.T) {
	fs, hub, hubPath, featPath := removeDefaultHub(t, defaultLocal)
	gitIn(t, hubPath, "worktree", "remove", "--force", featPath)
	require.NoError(t, hub.RemoveBranch("feat"))

	var err error
	_, stderr := captureStreams(t, func() {
		err = updateCurrentToDefault(fs, hub, hubPath, func(*hop.Hub, string) error {
			return errors.New("disk on fire")
		})
	})
	require.NoError(t, err)
	_, ok := currentLink(t, hubPath)
	assert.False(t, ok)
	assert.Contains(t, stderr, "disk on fire")
}

// (d) current is live on another hop: nothing is created, current stays.
func TestUpdateCurrentToDefault_LiveCurrentCreatesNothing(t *testing.T) {
	fs, hub, hubPath, featPath := removeDefaultHub(t, defaultLocal)
	otherPath := filepath.Join(hubPath, "hops", "other")
	gitIn(t, hubPath, "worktree", "add", "-q", "-b", "other", otherPath, "feat")
	require.NoError(t, hub.AddBranch("other", "other", otherPath))
	require.NoError(t, hop.UpdateCurrentSymlink(fs, hubPath, otherPath))
	// feat is removed; current names other.
	gitIn(t, hubPath, "worktree", "remove", "--force", featPath)
	require.NoError(t, hub.RemoveBranch("feat"))

	called := false
	err := updateCurrentToDefault(fs, hub, hubPath, func(*hop.Hub, string) error {
		called = true
		return nil
	})
	assert.Error(t, err, "today's warning for a live current stays")
	assert.False(t, called, "no creation while current is live")
	target, ok := currentLink(t, hubPath)
	require.True(t, ok)
	assert.Equal(t, "hops/other", target)
}

// (e) dry-run announces the creation and creates nothing.
func TestPreviewCurrentAfterRemoval_AnnouncesDefaultWorktreeCreation(t *testing.T) {
	for _, where := range []string{defaultLocal, defaultOrigin} {
		t.Run(where, func(t *testing.T) {
			fs, hub, hubPath, featPath := removeDefaultHub(t, where)

			stdout, _ := captureStreams(t, func() {
				previewCurrentAfterRemoval(fs, git.New(), hub, hubPath, featPath)
			})
			assert.Contains(t, stdout, "Would create worktree for default branch 'main' and point 'current' at it")
			assert.NoDirExists(t, filepath.Join(hubPath, "hops", "main"))
			assert.NotContains(t, hub.Config.Branches, "main")
			target, _ := currentLink(t, hubPath)
			assert.Equal(t, "hops/feat", target)
		})
	}
}

// (e) with nothing to create it from, dry-run says current is dropped.
func TestPreviewCurrentAfterRemoval_DefaultNowhereSaysDrop(t *testing.T) {
	fs, hub, hubPath, featPath := removeDefaultHub(t, defaultNowhere)
	stdout, _ := captureStreams(t, func() {
		previewCurrentAfterRemoval(fs, git.New(), hub, hubPath, featPath)
	})
	assert.NotContains(t, stdout, "Would create worktree")
	assert.Contains(t, stdout, "Would remove 'current'")
}

// (f) a hub with a default worktree behaves as before: current moves to
// it, nothing is created.
func TestRemove_HubWithDefaultWorktreeUnchanged(t *testing.T) {
	fs, hub, hubPath, _ := removeDefaultHub(t, defaultLocal)
	mainPath := filepath.Join(hubPath, "hops", "main")
	gitIn(t, hubPath, "worktree", "add", "-q", mainPath, "main")
	require.NoError(t, hub.AddBranch("main", "main", mainPath))

	_, err := removeBranchWorktreeOpts(fs, git.New(), hub, hubPath, "feat", removeOptions{})
	require.NoError(t, err)
	assert.NoDirExists(t, filepath.Join(hubPath, "hops", "other"))
	target, ok := currentLink(t, hubPath)
	require.True(t, ok)
	assert.Equal(t, "hops/main", target)

	stdout, _ := captureStreams(t, func() {
		previewCurrentAfterRemoval(fs, git.New(), hub, hubPath, mainPath)
	})
	assert.Contains(t, stdout, "Would point 'current' at 'main'")
}
