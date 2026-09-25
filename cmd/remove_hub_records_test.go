package cmd

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
)

// Removing a local hub beside a --global one leaves the data-home
// hopspace's hop.json alone: the local hub has no records there, and its
// removal is no reason to rewrite (migrate) the global hub's.
func TestRemoveHub_LocalHubLeavesSharedRecords(t *testing.T) {
	isolateDoctorPaths(t)
	fs := afero.NewMemMapFs()
	g1, local := "/hubs/g1", "/hubs/local"
	hubFixture(t, fs, g1, true)
	hubFixture(t, fs, local, false)
	hopspacePath := sharedHopspaceFixture(t, fs, map[string]bool{g1: true, local: false})
	hs, err := hop.LoadHopspace(fs, hopspacePath)
	require.NoError(t, err)
	// A record an earlier release keyed by branch: any write for a global
	// hub migrates it.
	require.NoError(t, config.NewWriter(fs).WriteHopspaceConfig(hopspacePath, &config.HopspaceConfig{
		Repo: hs.Config.Repo,
		Branches: map[string]config.HopspaceBranch{
			"main": {Exists: true, Path: filepath.Join(g1, "hops", "main")},
		},
	}))
	hopJSON := filepath.Join(hopspacePath, "hop.json")
	before, err := afero.ReadFile(fs, hopJSON)
	require.NoError(t, err)

	captureRemoveOutput(t, func() { previewRemoveHub(fs, local, true) })
	stdout, stderr := captureRemoveOutput(t, func() { removeHub(fs, local) })

	after, err := afero.ReadFile(fs, hopJSON)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after))
	assert.NotContains(t, stdout+stderr, "Dropped")
}
