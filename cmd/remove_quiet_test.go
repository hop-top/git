package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
	"hop.top/git/test/mocks"
)

// captureRemoveOutput runs fn with stdout and stderr redirected to pipes
// and returns what each stream received.
//
// output.Warn writes through the charm logger to stderr; output.Info
// writes to stdout. Warnings are the noise under test, so the two
// streams are kept separate: a benign condition must leave stderr clean
// while the stdout progress narration still reads sensibly.
func captureRemoveOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()

	origOut, origErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	require.NoError(t, err)
	errR, errW, err := os.Pipe()
	require.NoError(t, err)

	os.Stdout, os.Stderr = outW, errW
	// The package logger caches the stderr handle it was built with, so
	// rebuild it against the pipe now that os.Stderr points at it.
	output.SetupLogger(output.ModeHuman, false)

	outCh := make(chan string, 1)
	errCh := make(chan string, 1)
	go func() { var b bytes.Buffer; _, _ = io.Copy(&b, outR); outCh <- b.String() }()
	go func() { var b bytes.Buffer; _, _ = io.Copy(&b, errR); errCh <- b.String() }()

	func() {
		defer func() {
			outW.Close()
			errW.Close()
			os.Stdout, os.Stderr = origOut, origErr
			output.SetupLogger(output.ModeHuman, false)
		}()
		fn()
	}()

	return <-outCh, <-errCh
}

// newQuietRemoveHub builds a fully healthy hub for warning-noise tests:
// a real temp dir (so the `current` symlink update actually succeeds)
// and a pre-registered entry in global state (so the state update
// succeeds too).
//
// Both matter because these tests assert stderr is completely clean. A
// fixture that left either subsystem broken would emit its own genuine
// warnings and mask the ones under test.
func newQuietRemoveHub(t *testing.T) (afero.Fs, *hop.Hub, string, string) {
	t.Helper()

	// Redirect XDG state into the test's temp dir so LoadState/SaveState
	// never touch the developer's real state file.
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	fs := afero.NewOsFs()
	hubPath := t.TempDir()

	hub, err := hop.CreateHub(fs, hubPath, "git@github.com:test/repo.git", "test", "repo", "main")
	require.NoError(t, err)

	mainPath := filepath.Join(hubPath, "hops", "main")
	require.NoError(t, fs.MkdirAll(mainPath, 0o755))
	require.NoError(t, hub.AddBranch("main", "main", mainPath))

	featurePath := filepath.Join(hubPath, "hops", "feature")
	require.NoError(t, fs.MkdirAll(featurePath, 0o755))
	require.NoError(t, hub.AddBranch("feature", "feature", featurePath))

	// Register the repo + both worktrees in global state.
	st, err := state.LoadState(fs)
	require.NoError(t, err)
	st.AddRepository("github.com/test/repo", &state.RepositoryState{
		Org:           "test",
		Repo:          "repo",
		DefaultBranch: "main",
		Worktrees: map[string]*state.WorktreeState{
			"main":    {Path: mainPath, Type: "linked", HubPath: hubPath},
			"feature": {Path: featurePath, Type: "linked", HubPath: hubPath},
		},
	})
	require.NoError(t, state.SaveState(fs, st))

	return fs, hub, hubPath, featurePath
}

// porcelainFor renders a `git worktree list --porcelain` payload that
// registers each given path. Used to put the mock's worktree registry in
// a known state.
func porcelainFor(paths ...string) string {
	var b strings.Builder
	for i, p := range paths {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "worktree %s\nHEAD 0000000000000000000000000000000000000000\nbranch refs/heads/b%d\n", p, i)
	}
	return b.String()
}

// TestRemoveBranchWorktree_AlreadyAbsentIsQuiet pins the reported bug:
// removing a branch whose worktree is no longer registered with git and
// whose local branch no longer exists emitted two WARN lines and then
// reported success. Both conditions ARE the desired end state, so
// neither is a warning.
func TestRemoveBranchWorktree_AlreadyAbsentIsQuiet(t *testing.T) {
	fs, hub, hubPath, _ := newQuietRemoveHub(t)

	mockGit := mocks.NewMockGit()
	// Worktree registry knows only the default branch: the feature
	// worktree is already deregistered.
	mockGit.WorktreeListOut = porcelainFor(filepath.Join(hubPath, "hops", "main"))
	// No local branches at all: `feature` is already deleted.
	mockGit.LocalBranches = nil
	// Both operations would fail if attempted, exactly as real git does.
	mockGit.WorktreeRemoveErr = errors.New("fatal: is not a working tree")
	mockGit.DeleteLocalBranchErr = errors.New("error: branch 'feature' not found.")

	var runErr error
	stdout, stderr := captureRemoveOutput(t, func() {
		runErr = removeBranchWorktree(fs, mockGit, hub, hubPath, "feature")
	})
	require.NoError(t, runErr)

	assert.NotContains(t, stderr, "warning:",
		"already-absent worktree/branch must not warn; stderr was:\n%s", stderr)
	assert.NotContains(t, strings.ToUpper(stderr), "WARN",
		"already-absent worktree/branch must not warn; stderr was:\n%s", stderr)

	// The removal must not even attempt the doomed git calls.
	assert.Empty(t, mockGit.WorktreeRemoveCalls,
		"must not run `git worktree remove` on an unregistered worktree")
	assert.Empty(t, mockGit.DeletedLocalBranches,
		"must not run `git branch -D` on a branch that does not exist")

	// The success path stays honest: the end state was achieved.
	assert.Contains(t, stdout, "Successfully removed feature")
	assert.NotContains(t, hub.Config.Branches, "feature")
}

// TestRemoveBranchWorktree_GenuineWorktreeFailureWarns is the guard
// against over-silencing. The worktree IS registered, so removal is
// attempted; when git refuses (locked worktree, permission denied) the
// user must still see it.
func TestRemoveBranchWorktree_GenuineWorktreeFailureWarns(t *testing.T) {
	fs, hub, hubPath, featurePath := newQuietRemoveHub(t)

	mockGit := mocks.NewMockGit()
	// Registry lists the feature worktree: it is live, not already gone.
	mockGit.WorktreeListOut = porcelainFor(filepath.Join(hubPath, "hops", "main"), featurePath)
	mockGit.LocalBranches = []string{"main"}
	mockGit.WorktreeRemoveErr = errors.New("fatal: validation failed, cannot remove working tree")

	var runErr error
	_, stderr := captureRemoveOutput(t, func() {
		runErr = removeBranchWorktree(fs, mockGit, hub, hubPath, "feature")
	})
	require.NoError(t, runErr)

	assert.NotEmpty(t, mockGit.WorktreeRemoveCalls,
		"a registered worktree must still be handed to `git worktree remove`")
	assert.Contains(t, stderr, "cannot remove working tree",
		"a genuine worktree-removal failure must stay visible; stderr was:\n%s", stderr)
}

// TestRemoveBranchWorktree_GenuineBranchFailureWarns is the second
// over-silencing guard. The branch exists, so deletion is attempted;
// when git refuses (locked ref, permission denied) the user must see it.
func TestRemoveBranchWorktree_GenuineBranchFailureWarns(t *testing.T) {
	fs, hub, hubPath, _ := newQuietRemoveHub(t)

	mockGit := mocks.NewMockGit()
	mockGit.WorktreeListOut = porcelainFor(filepath.Join(hubPath, "hops", "main"))
	// The branch DOES exist, so deletion must be attempted.
	mockGit.LocalBranches = []string{"main", "feature"}
	mockGit.DeleteLocalBranchErr = errors.New("error: cannot lock ref 'refs/heads/feature'")

	var runErr error
	_, stderr := captureRemoveOutput(t, func() {
		runErr = removeBranchWorktree(fs, mockGit, hub, hubPath, "feature")
	})
	require.NoError(t, runErr)

	assert.Contains(t, mockGit.DeletedLocalBranches, "feature",
		"an existing branch must still be handed to `git branch -D`")
	assert.Contains(t, stderr, "cannot lock ref",
		"a genuine branch-deletion failure must stay visible; stderr was:\n%s", stderr)
}
