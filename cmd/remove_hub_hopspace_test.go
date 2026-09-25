package cmd

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/state"
)

// hubFixture writes a hub of test/repo at path with its main worktree,
// marked global (clone --global) or not.
func hubFixture(t *testing.T, fs afero.Fs, path string, global bool) {
	t.Helper()
	writePruneHub(t, fs, path, []string{"main"}, []string{"main"})
	if !global {
		return
	}
	hub, err := hop.LoadHub(fs, path)
	require.NoError(t, err)
	require.NoError(t, hub.Update(func(cfg *config.HubConfig) error {
		cfg.Repo.Mode = config.RepoModeGlobal
		return nil
	}))
}

// sharedHopspaceFixture writes the data-home hopspace of test/repo and a
// state recording hubs (path -> global?) as the repository's hubs.
func sharedHopspaceFixture(t *testing.T, fs afero.Fs, hubs map[string]bool) string {
	t.Helper()
	hopspacePath := hop.GetHopspacePath(hop.GetGitHopDataHome(), hop.RepoRef{Org: "test", Repo: "repo"})
	_, err := hop.InitHopspace(fs, hopspacePath, "git@github.com:test/repo.git", "test", "repo", "main")
	require.NoError(t, err)

	st := stateWithHub("/unused")
	repo := st.Repositories["github.com/test/repo"]
	repo.Hubs = nil
	for path, global := range hubs {
		mode := state.HubModeLocal
		if global {
			mode = state.HubModeGlobal
		}
		repo.Hubs = append(repo.Hubs, &state.HubState{Path: path, Mode: mode})
	}
	require.NoError(t, state.SaveState(fs, st))
	return hopspacePath
}

// Two --global hubs of one repository share the data-home hopspace.
// Removing one used to delete it, leaving the other hub unable to load
// its hopspace. It is kept while another hub uses it and removed with
// the last one.
func TestRemoveHub_GlobalHopspaceKeptWhileAnotherHubUsesIt(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	g1, g2 := "/hubs/g1", "/hubs/g2"
	hubFixture(t, fs, g1, true)
	hubFixture(t, fs, g2, true)
	hopspacePath := sharedHopspaceFixture(t, fs, map[string]bool{g1: true, g2: true})

	stdout, stderr := captureRemoveOutput(t, func() { removeHub(fs, g1) })

	exists, _ := afero.DirExists(fs, hopspacePath)
	require.True(t, exists, "the hopspace g2 uses must survive g1's removal")
	other, err := hop.LoadHub(fs, g2)
	require.NoError(t, err)
	_, err = hop.LoadHopspace(fs, hop.ResolveHopspacePath(g2, other.Config.Repo))
	assert.NoError(t, err, "g2 still loads its hopspace")
	assert.Contains(t, stdout+stderr, "still used by "+g2)
	gone, _ := afero.DirExists(fs, g1)
	assert.False(t, gone, "g1 itself is removed")

	removeHub(fs, g2)

	exists, _ = afero.DirExists(fs, hopspacePath)
	assert.False(t, exists, "the last hub's removal deletes the hopspace")
}

// --dry-run says whether the hopspace would be kept or deleted, and why,
// and touches nothing.
func TestPreviewRemoveHub_SaysWhetherHopspaceIsKept(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	g1, g2 := "/hubs/g1", "/hubs/g2"
	hubFixture(t, fs, g1, true)
	hubFixture(t, fs, g2, true)
	hopspacePath := sharedHopspaceFixture(t, fs, map[string]bool{g1: true, g2: true})

	stdout, stderr := captureRemoveOutput(t, func() { previewRemoveHub(fs, g1, true) })
	assert.Contains(t, stdout+stderr, "Would keep hopspace data at "+hopspacePath)
	assert.Contains(t, stdout+stderr, "still used by "+g2)
	assert.NotContains(t, stdout+stderr, "Would remove hopspace data")

	sharedHopspaceFixture(t, fs, map[string]bool{g1: true})
	stdout, stderr = captureRemoveOutput(t, func() { previewRemoveHub(fs, g1, true) })
	assert.Contains(t, stdout+stderr, "Would remove hopspace data at "+hopspacePath)
	assert.Contains(t, stdout+stderr, "no other hub uses it")

	for _, p := range []string{g1, g2, hopspacePath} {
		exists, _ := afero.DirExists(fs, p)
		assert.True(t, exists, "dry-run must not remove %s", p)
	}
}

// Which hubs use the hopspace is decided by each hub's own marker, as
// hop.ResolveHopspacePath does. A hub state records but whose directory
// is gone uses nothing; a local hub does not use the data-home hopspace,
// and removing one must not delete it from under a global hub either.
func TestRemoveHub_HopspaceUsers(t *testing.T) {
	t.Run("recorded hub whose directory is gone", func(t *testing.T) {
		isolateDoctorPaths(t)
		fs := afero.NewMemMapFs()
		g1 := "/hubs/g1"
		hubFixture(t, fs, g1, true)
		hopspacePath := sharedHopspaceFixture(t, fs, map[string]bool{g1: true, "/hubs/gone": true})

		removeHub(fs, g1)

		exists, _ := afero.DirExists(fs, hopspacePath)
		assert.False(t, exists, "a hub that is gone does not keep the hopspace")
	})
	t.Run("remaining hub is local", func(t *testing.T) {
		isolateDoctorPaths(t)
		fs := afero.NewMemMapFs()
		g1, local := "/hubs/g1", "/hubs/local"
		hubFixture(t, fs, g1, true)
		hubFixture(t, fs, local, false)
		hopspacePath := sharedHopspaceFixture(t, fs, map[string]bool{g1: true, local: false})

		removeHub(fs, g1)

		exists, _ := afero.DirExists(fs, hopspacePath)
		assert.False(t, exists, "a local hub keeps its hopspace in itself")
	})
	t.Run("recorded global hub with an unreadable hop.json", func(t *testing.T) {
		isolateDoctorPaths(t)
		fs := afero.NewMemMapFs()
		g1, g2 := "/hubs/g1", "/hubs/g2"
		hubFixture(t, fs, g1, true)
		require.NoError(t, afero.WriteFile(fs, filepath.Join(g2, "hop.json"), []byte("{not json"), 0o644))
		hopspacePath := sharedHopspaceFixture(t, fs, map[string]bool{g1: true, g2: true})

		removeHub(fs, g1)

		exists, _ := afero.DirExists(fs, hopspacePath)
		assert.True(t, exists, "state's mode stands in for the marker it cannot read")
	})
	t.Run("state unreadable", func(t *testing.T) {
		isolateDoctorPaths(t)
		fs := afero.NewMemMapFs()
		g1 := "/hubs/g1"
		hubFixture(t, fs, g1, true)
		hopspacePath := sharedHopspaceFixture(t, fs, map[string]bool{g1: true})
		require.NoError(t, afero.WriteFile(fs, filepath.Join(state.GetStateHome(), "state.json"), []byte("{not json"), 0o644))

		stdout, stderr := captureRemoveOutput(t, func() { removeHub(fs, g1) })

		exists, _ := afero.DirExists(fs, hopspacePath)
		assert.True(t, exists, "which hubs use the hopspace is unknown, so it is kept")
		assert.Contains(t, stdout+stderr, "cannot tell whether another hub uses it")
	})
	t.Run("removing a local hub beside a global one", func(t *testing.T) {
		isolateDoctorPaths(t)
		fs := afero.NewMemMapFs()
		g1, local := "/hubs/g1", "/hubs/local"
		hubFixture(t, fs, g1, true)
		hubFixture(t, fs, local, false)
		hopspacePath := sharedHopspaceFixture(t, fs, map[string]bool{g1: true, local: false})

		removeHub(fs, local)

		exists, _ := afero.DirExists(fs, hopspacePath)
		assert.True(t, exists, "g1 still uses the data-home hopspace")
	})
}
