package services

import (
	"os"
	"path/filepath"
	"testing"

	"hop.top/git/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadEnvManagers(t *testing.T) {
	t.Run("loads built-in docker-compose manager", func(t *testing.T) {
		managers, err := LoadEnvManagers(nil)
		require.NoError(t, err)
		require.Len(t, managers, 1)

		docker := managers[0]
		assert.Equal(t, "docker-compose", docker.Name)
		assert.Contains(t, docker.DetectFiles, "compose.yaml")
		assert.Contains(t, docker.DetectFiles, "docker-compose.yml")
		assert.Equal(t, []string{"docker", "compose", "up", "-d"}, docker.Commands.Start)
		assert.Equal(t, []string{"docker", "compose", "stop"}, docker.Commands.Stop)
	})

	t.Run("loads custom managers from config", func(t *testing.T) {
		globalConfig := &config.GlobalConfig{
			EnvironmentManagers: []config.EnvManagerConfig{
				{
					Name:        "podman-compose",
					DetectFiles: []string{"compose.yaml"},
					Commands: config.EnvCommands{
						Start: []string{"podman-compose", "up", "-d"},
						Stop:  []string{"podman-compose", "stop"},
					},
				},
			},
		}

		managers, err := LoadEnvManagers(globalConfig)
		require.NoError(t, err)
		require.Len(t, managers, 2) // built-in + custom

		// Find podman manager
		var podman *EnvironmentManager
		for i := range managers {
			if managers[i].Name == "podman-compose" {
				podman = &managers[i]
				break
			}
		}

		require.NotNil(t, podman)
		assert.Equal(t, "podman-compose", podman.Name)
		assert.Equal(t, []string{"podman-compose", "up", "-d"}, podman.Commands.Start)
	})

	t.Run("loads custom manager with hooks", func(t *testing.T) {
		globalConfig := &config.GlobalConfig{
			EnvironmentManagers: []config.EnvManagerConfig{
				{
					Name:        "custom",
					DetectFiles: []string{"custom.conf"},
					Commands: config.EnvCommands{
						Start: []string{"custom-start"},
						Stop:  []string{"custom-stop"},
					},
					Hooks: config.EnvHooks{
						PreStart:  []string{"scripts/pre.sh"},
						PostStart: []string{"scripts/post.sh"},
					},
				},
			},
		}

		managers, err := LoadEnvManagers(globalConfig)
		require.NoError(t, err)

		// Find custom manager
		var custom *EnvironmentManager
		for i := range managers {
			if managers[i].Name == "custom" {
				custom = &managers[i]
				break
			}
		}

		require.NotNil(t, custom)
		assert.Equal(t, []string{"scripts/pre.sh"}, custom.Hooks.PreStart)
		assert.Equal(t, []string{"scripts/post.sh"}, custom.Hooks.PostStart)
	})
}

func TestDetectEnvManager(t *testing.T) {
	// Create temp directory for testing
	tmpDir, err := os.MkdirTemp("", "env-manager-test-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	managers := []EnvironmentManager{
		{
			Name:        "docker-compose",
			DetectFiles: []string{"compose.yaml", "docker-compose.yml"},
		},
		{
			Name:        "podman-compose",
			DetectFiles: []string{"podman-compose.yaml"},
		},
	}

	t.Run("detects docker-compose via compose.yaml", func(t *testing.T) {
		// Create compose.yaml
		composePath := filepath.Join(tmpDir, "compose.yaml")
		err := os.WriteFile(composePath, []byte("services:\n"), 0644)
		require.NoError(t, err)
		defer os.Remove(composePath)

		manager, err := DetectEnvManager(tmpDir, nil, managers)
		require.NoError(t, err)
		require.NotNil(t, manager)
		assert.Equal(t, "docker-compose", manager.Name)
	})

	t.Run("detects docker-compose via docker-compose.yml", func(t *testing.T) {
		// Create docker-compose.yml
		composePath := filepath.Join(tmpDir, "docker-compose.yml")
		err := os.WriteFile(composePath, []byte("services:\n"), 0644)
		require.NoError(t, err)
		defer os.Remove(composePath)

		manager, err := DetectEnvManager(tmpDir, nil, managers)
		require.NoError(t, err)
		require.NotNil(t, manager)
		assert.Equal(t, "docker-compose", manager.Name)
	})

	t.Run("returns nil when no detect files found", func(t *testing.T) {
		manager, err := DetectEnvManager(tmpDir, nil, managers)
		require.NoError(t, err)
		assert.Nil(t, manager)
	})

	t.Run("respects explicit override in repo config", func(t *testing.T) {
		managerName := "podman-compose"
		repoConfig := &config.HubConfig{
			Settings: config.HubSettings{
				EnvironmentManager: &managerName,
			},
		}

		manager, err := DetectEnvManager(tmpDir, repoConfig, managers)
		require.NoError(t, err)
		require.NotNil(t, manager)
		assert.Equal(t, "podman-compose", manager.Name)
	})

	t.Run("returns nil when override is 'none'", func(t *testing.T) {
		managerName := "none"
		repoConfig := &config.HubConfig{
			Settings: config.HubSettings{
				EnvironmentManager: &managerName,
			},
		}

		manager, err := DetectEnvManager(tmpDir, repoConfig, managers)
		require.NoError(t, err)
		assert.Nil(t, manager)
	})

	t.Run("errors when override manager not found", func(t *testing.T) {
		managerName := "nonexistent"
		repoConfig := &config.HubConfig{
			Settings: config.HubSettings{
				EnvironmentManager: &managerName,
			},
		}

		manager, err := DetectEnvManager(tmpDir, repoConfig, managers)
		require.Error(t, err)
		assert.Nil(t, manager)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("detects first match when multiple files present", func(t *testing.T) {
		// Create both compose.yaml and podman-compose.yaml
		compose1 := filepath.Join(tmpDir, "compose.yaml")
		compose2 := filepath.Join(tmpDir, "podman-compose.yaml")
		err := os.WriteFile(compose1, []byte("services:\n"), 0644)
		require.NoError(t, err)
		err = os.WriteFile(compose2, []byte("services:\n"), 0644)
		require.NoError(t, err)
		defer os.Remove(compose1)
		defer os.Remove(compose2)

		manager, err := DetectEnvManager(tmpDir, nil, managers)
		require.NoError(t, err)
		require.NotNil(t, manager)
		// Should detect first manager (docker-compose) since it's first in the list
		assert.Equal(t, "docker-compose", manager.Name)
	})
}

func TestEnvironmentManagerValidate(t *testing.T) {
	t.Run("validates successfully with all required fields", func(t *testing.T) {
		manager := &EnvironmentManager{
			Name:        "docker-compose",
			DetectFiles: []string{"compose.yaml"},
			Commands: EnvCommands{
				Start: []string{"docker", "compose", "up"},
				Stop:  []string{"docker", "compose", "stop"},
			},
		}

		err := manager.Validate()
		assert.NoError(t, err)
	})

	t.Run("errors when name is empty", func(t *testing.T) {
		manager := &EnvironmentManager{
			Name:        "",
			DetectFiles: []string{"compose.yaml"},
			Commands: EnvCommands{
				Start: []string{"docker", "compose", "up"},
				Stop:  []string{"docker", "compose", "stop"},
			},
		}

		err := manager.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name is required")
	})

	t.Run("errors when detect files is empty", func(t *testing.T) {
		manager := &EnvironmentManager{
			Name:        "docker-compose",
			DetectFiles: []string{},
			Commands: EnvCommands{
				Start: []string{"docker", "compose", "up"},
				Stop:  []string{"docker", "compose", "stop"},
			},
		}

		err := manager.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "detect file is required")
	})

	t.Run("errors when start command is empty", func(t *testing.T) {
		manager := &EnvironmentManager{
			Name:        "docker-compose",
			DetectFiles: []string{"compose.yaml"},
			Commands: EnvCommands{
				Start: []string{},
				Stop:  []string{"docker", "compose", "stop"},
			},
		}

		err := manager.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "start command is required")
	})

	t.Run("errors when stop command is empty", func(t *testing.T) {
		manager := &EnvironmentManager{
			Name:        "docker-compose",
			DetectFiles: []string{"compose.yaml"},
			Commands: EnvCommands{
				Start: []string{"docker", "compose", "up"},
				Stop:  []string{},
			},
		}

		err := manager.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stop command is required")
	})

	t.Run("allows optional health command", func(t *testing.T) {
		manager := &EnvironmentManager{
			Name:        "docker-compose",
			DetectFiles: []string{"compose.yaml"},
			Commands: EnvCommands{
				Start:  []string{"docker", "compose", "up"},
				Stop:   []string{"docker", "compose", "stop"},
				Health: []string{"docker", "compose", "ps"},
			},
		}

		err := manager.Validate()
		assert.NoError(t, err)
	})
}
