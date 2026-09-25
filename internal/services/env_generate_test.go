package services

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/config"
	"hop.top/git/internal/docker"
)

// Generating a worktree's environment again keeps the ports it has, in
// incremental mode too, where new ports go after the highest one in use:
// the worktree's own ports, and those of a worktree allocated after it,
// must not push it up a block.
func TestGenerateWorktreeEnv_RegenerateKeepsPorts(t *testing.T) {
	fs := afero.NewMemMapFs()
	d := docker.New(docker.WithRunner(composeConfigRunner{out: "services:\n  web: {}\n"}))
	hub := "/src/app"
	main := filepath.Join(hub, "hops", "main")
	feat := filepath.Join(hub, "hops", "feat")
	for _, wt := range []string{main, feat} {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(wt, "compose.yaml"), []byte(hardcodedPortsCompose), 0644))
	}
	generate := func(wt, branch string) map[string]int {
		t.Helper()
		env, err := GenerateWorktreeEnv(fs, d, hub, hub, wt, branch, "acme", "svc")
		require.NoError(t, err)
		require.NotNil(t, env)
		return env.Ports.Ports
	}

	first := generate(main, "main")
	require.NotEmpty(t, first)
	assert.Equal(t, first, generate(main, "main"), "regenerating alone moved the ports")

	other := generate(feat, "feat")
	assert.NotEqual(t, first["WEB"], other["WEB"], "two worktrees got one port")

	for i := 0; i < 3; i++ {
		assert.Equal(t, first, generate(main, "main"), "regenerating after another worktree moved the ports")
		assert.Equal(t, other, generate(feat, "feat"), "regenerating the newest worktree moved its ports")
	}

	cfg, err := config.NewLoader(fs).LoadPortsConfig(hub)
	require.NoError(t, err)
	assert.Equal(t, "incremental", cfg.AllocationMode)
	assert.Equal(t, first, cfg.Branches["main"].Ports)
	assert.Equal(t, other, cfg.Branches["feat"].Ports)
}
