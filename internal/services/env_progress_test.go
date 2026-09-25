package services

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/config"
	"hop.top/git/internal/output"
)

// A target's Progress writer is where its manager, and so the manager's
// hooks and lifecycle command, write, whatever the output mode.
func TestResolveEnv_ProgressWriterReceivesManagerOutput(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "compose.yaml"), []byte("services: {}\n"), 0o644))
	var progress bytes.Buffer

	manager, _, err := ResolveEnv(EnvTarget{Root: root, Progress: &progress}, nil)
	require.NoError(t, err)
	require.NotNil(t, manager)
	assert.Same(t, &progress, manager.Out)

	manager, _, err = ResolveEnv(EnvTarget{Root: root}, nil)
	require.NoError(t, err)
	assert.Nil(t, manager.Out, "without Progress the caller's streams stay in charge")
}

// Hook output and the lifecycle command's stdout reach the manager's Out.
func TestManagerStart_WritesToOut(t *testing.T) {
	root := t.TempDir()
	hook := filepath.Join(root, "pre.sh")
	require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\necho hook-said\n"), 0o755))
	var out bytes.Buffer
	m := &EnvironmentManager{
		Name:     "custom",
		Commands: EnvCommands{Start: []string{"echo", "command-said"}},
		Hooks:    EnvHooks{PreStart: []string{"pre.sh"}},
		Out:      &out,
	}
	require.NoError(t, m.Start(root, "main", "", (*config.HubConfig)(nil), ""))
	assert.Contains(t, out.String(), "hook-said")
	assert.Contains(t, out.String(), "command-said")
	assert.Contains(t, out.String(), "Environment started successfully")
}

// The manager's step lines are progress for a person at a terminal: -q
// and the structured formats drop them, as they drop output.Note. What
// the lifecycle command itself prints is not a step line and still
// reaches Out.
func TestManagerSteps_FollowOutputMode(t *testing.T) {
	for _, tt := range []struct {
		name  string
		mode  output.Mode
		steps bool
	}{
		{"human", output.ModeHuman, true},
		{"quiet", output.ModeQuiet, false},
		{"json", output.ModeJSON, false},
		{"porcelain", output.ModePorcelain, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			prev := output.CurrentMode
			output.CurrentMode = tt.mode
			t.Cleanup(func() { output.CurrentMode = prev })

			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "pre.sh"), []byte("#!/bin/sh\necho hook-said\n"), 0o755))
			var out bytes.Buffer
			m := &EnvironmentManager{
				Name:     "custom",
				Commands: EnvCommands{Start: []string{"echo", "start-said"}, Stop: []string{"echo", "stop-said"}},
				Hooks:    EnvHooks{PreStart: []string{"pre.sh"}},
				Out:      &out,
			}
			require.NoError(t, m.Start(root, "main", "", nil, ""))
			require.NoError(t, m.Stop(root, "main", "", nil, ""))

			assert.Contains(t, out.String(), "start-said")
			assert.Contains(t, out.String(), "stop-said")
			for _, step := range []string{
				"Running preStart hooks",
				"Starting services: custom",
				"Environment started successfully",
				"Stopping services: custom",
				"Environment stopped successfully",
			} {
				assert.Equal(t, tt.steps, strings.Contains(out.String(), step), "step %q", step)
			}
		})
	}
}

// Under -q a start or stop holds back what its hooks and lifecycle
// command print: dropped on success, replayed on Err when it fails, so a
// failure is never silent. Without -q it streams live.
func TestManagerQuiet_HoldsBackOutputUntilFailure(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "pre.sh"), []byte("#!/bin/sh\necho hook-said\n"), 0o755))
	newManager := func(start []string) (*EnvironmentManager, *bytes.Buffer, *bytes.Buffer) {
		var out, errOut bytes.Buffer
		return &EnvironmentManager{
			Name:     "custom",
			Commands: EnvCommands{Start: start, Stop: start},
			Hooks:    EnvHooks{PreStart: []string{"pre.sh"}, PreStop: []string{"pre.sh"}},
			Out:      &out,
			Err:      &errOut,
		}, &out, &errOut
	}
	succeed := []string{"sh", "-c", "echo said-out; echo said-err >&2"}
	fail := []string{"sh", "-c", "echo said-out; echo said-err >&2; exit 3"}

	t.Run("quiet success", func(t *testing.T) {
		setMode(t, output.ModeQuiet)
		m, out, errOut := newManager(succeed)
		require.NoError(t, m.Start(root, "main", "", nil, ""))
		require.NoError(t, m.Stop(root, "main", "", nil, ""))
		assert.Empty(t, out.String())
		assert.Empty(t, errOut.String())
	})
	t.Run("quiet failure", func(t *testing.T) {
		setMode(t, output.ModeQuiet)
		for name, run := range map[string]func(*EnvironmentManager) error{
			"start": func(m *EnvironmentManager) error { return m.Start(root, "main", "", nil, "") },
			"stop":  func(m *EnvironmentManager) error { return m.Stop(root, "main", "", nil, "") },
		} {
			m, out, errOut := newManager(fail)
			require.Error(t, run(m), name)
			assert.Empty(t, out.String(), name)
			assert.Contains(t, errOut.String(), "hook-said", name)
			assert.Contains(t, errOut.String(), "said-out\nsaid-err\n", name)
		}
	})
	t.Run("not quiet streams live", func(t *testing.T) {
		setMode(t, output.ModeHuman)
		m, out, errOut := newManager(succeed)
		require.NoError(t, m.Start(root, "main", "", nil, ""))
		assert.Contains(t, out.String(), "hook-said")
		assert.Contains(t, out.String(), "said-out")
		assert.Equal(t, "said-err\n", errOut.String())
	})
}

// setMode sets the output mode, -q included, for the rest of t.
func setMode(t *testing.T, mode output.Mode) {
	t.Helper()
	prev := output.CurrentMode
	output.SetupLogger(mode, false)
	t.Cleanup(func() { output.SetupLogger(prev, false) })
}
