package cmd

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

// captureStdout redirects stdout during f and returns what was printed.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()
	f()
	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

// isolateDataHome points both data-home knobs at a fresh temp dir.
// handleAlreadyInitialized mirrors hooks into a hopspace resolved from
// GIT_HOP_DATA_HOME, falling back to $XDG_DATA_HOME/git-hop, so both must
// move or the mirror writes into the developer's real data home.
func isolateDataHome(t *testing.T) string {
	t.Helper()
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("GIT_HOP_DATA_HOME", dataHome)
	return dataHome
}

// hopspaceHooksDirs returns the hopspace hook dirs the mirror step
// created under dataHome (<dataHome>/<host>/<org>/<repo>/hooks).
func hopspaceHooksDirs(t *testing.T, dataHome string) []string {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(dataHome, "*", "*", "*", "hooks"))
	require.NoError(t, err)
	return dirs
}

func TestHandleAlreadyInitialized_BareWorktreeRoot(t *testing.T) {
	dataHome := isolateDataHome(t)

	// A real bare hub: bareness is git's answer, so a .git dir that merely
	// has a worktrees/ subdir no longer passes for one.
	dir := t.TempDir()
	seed := filepath.Join(dir, "seed")
	repoPath := filepath.Join(dir, "hub")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main", seed},
		{"-C", seed, "commit", "-q", "--allow-empty", "-m", "init"},
		{"clone", "-q", "--bare", seed, repoPath},
		{"-C", repoPath, "worktree", "add", "-q", "hops/main", "main"},
	} {
		out, err := exec.Command("git", args...).CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	g := git.New()
	fs := afero.NewOsFs()

	structure := hop.DetectRepoStructure(fs, g, repoPath)
	assert.Equal(t, config.BareWorktreeRoot, structure)

	out := captureStdout(t, func() {
		handleAlreadyInitialized(fs, g, repoPath, structure)
	})

	assert.Contains(t, out, "already initialized")
	// The hook mirror resolves a hopspace from the data home; it must land
	// in the one this test owns, never the developer's real one.
	assert.NotEmpty(t, hopspaceHooksDirs(t, dataHome),
		"hook mirror did not write under the test's data home %q", dataHome)
}

func TestHandleAlreadyInitialized_WorktreeChild(t *testing.T) {
	dataHome := isolateDataHome(t)

	// IsWorktree uses os.Stat, so we need a real tmpdir.
	worktreePath := t.TempDir()
	g := git.New()
	fs := afero.NewOsFs()

	// Simulate a WorktreeChild: .git is a file with "gitdir:" content
	require.NoError(t, os.WriteFile(filepath.Join(worktreePath, ".git"),
		[]byte("gitdir: /some/hub/.git/worktrees/feature-x\n"), 0644))

	structure := hop.DetectRepoStructure(fs, g, worktreePath)
	assert.Equal(t, config.WorktreeChild, structure)

	out := captureStdout(t, func() {
		handleAlreadyInitialized(fs, g, worktreePath, structure)
	})

	assert.Contains(t, out, "already initialized")
	// The hook mirror resolves a hopspace from the data home; it must land
	// in the one this test owns, never the developer's real one.
	assert.NotEmpty(t, hopspaceHooksDirs(t, dataHome),
		"hook mirror did not write under the test's data home %q", dataHome)
}
