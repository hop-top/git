package services

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/config"
)

// A hopspace two --global hubs share, plus an entry an earlier release
// wrote (no hub named) and a g1 entry without volumes.
func sharedEnvHopspace(t *testing.T, fs afero.Fs, hs string) {
	t.Helper()
	w := config.NewWriter(fs)
	require.NoError(t, w.WritePortsConfig(hs, &config.PortsConfig{Branches: map[string]config.BranchPorts{
		"/g1/app/hops/main":  {Ports: map[string]int{"WEB": 10000}, Branch: "main", Worktree: "/g1/app/hops/main", Hub: "/g1/app"},
		"/g1/app/hops/nodev": {Ports: map[string]int{"WEB": 10004}, Branch: "nodev", Worktree: "/g1/app/hops/nodev", Hub: "/g1/app"},
		"/g2/app/hops/main":  {Ports: map[string]int{"WEB": 10001}, Branch: "main", Worktree: "/g2/app/hops/main", Hub: "/g2/app"},
		"legacy":             {Ports: map[string]int{"WEB": 10002}},
	}}))
	require.NoError(t, w.WriteVolumesConfig(hs, &config.VolumesConfig{Branches: map[string]config.BranchVolumes{
		"/g1/app/hops/main": {Volumes: map[string]string{"data": filepath.Join(hs, "volumes", "g1", "hop_main_data")}},
		"/g2/app/hops/main": {Volumes: map[string]string{"data": filepath.Join(hs, "volumes", "g2", "hop_main_data")}},
		"legacy":            {Volumes: map[string]string{"data": filepath.Join(hs, "volumes", "hop_legacy_data")}},
	}}))
	for _, d := range []string{"g1/hop_main_data", "g2/hop_main_data"} {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(hs, "volumes", d, "kept"), []byte("x"), 0o644))
	}
}

func TestDropHubEnvEntries_DropsOnlyTheHubsRecords(t *testing.T) {
	fs := afero.NewMemMapFs()
	hs := "/data/org/app"
	sharedEnvHopspace(t, fs, hs)

	want := []EnvEntry{
		{Key: "/g1/app/hops/main", Branch: "main", Worktree: "/g1/app/hops/main", Volumes: true},
		{Key: "/g1/app/hops/nodev", Branch: "nodev", Worktree: "/g1/app/hops/nodev"},
	}
	assert.Equal(t, want, HubEnvEntries(fs, hs, "/g1/app"))

	got, err := DropHubEnvEntries(fs, hs, "/g1/app")
	require.NoError(t, err)
	assert.Equal(t, want, got)

	ports, err := config.NewLoader(fs).LoadPortsConfig(hs)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"/g2/app/hops/main", "legacy"}, keysOf(ports.Branches))
	vols, err := config.NewLoader(fs).LoadVolumesConfig(hs)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"/g2/app/hops/main", "legacy"}, keysOf(vols.Branches))

	// Records only: every volume directory keeps its data.
	for _, d := range []string{"g1/hop_main_data", "g2/hop_main_data"} {
		ok, _ := afero.Exists(fs, filepath.Join(hs, "volumes", d, "kept"))
		assert.True(t, ok, d)
	}

	// Nothing of the hub left: nothing to drop, nothing written.
	assert.Empty(t, HubEnvEntries(fs, hs, "/g1/app"))
	got, err = DropHubEnvEntries(fs, hs, "/g1/app")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestHubEnvEntries_NoPortsFile(t *testing.T) {
	fs := afero.NewMemMapFs()
	assert.Empty(t, HubEnvEntries(fs, "/data/none", "/g1/app"))
	got, err := DropHubEnvEntries(fs, "/data/none", "/g1/app")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
