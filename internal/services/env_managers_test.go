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

// TestComposeProjectName verifies that ComposeProjectName produces a stable,
// hop-scoped, compose-safe slug from (org, repo, branch). Compose requires
// project names to start with alphanumeric and contain only [a-z0-9_-].
func TestComposeProjectName(t *testing.T) {
	cases := []struct {
		name   string
		org    string
		repo   string
		branch string
		want   string
	}{
		{"simple", "hop-top", "git", "main", "hop-top-git-main"},
		{"slash branch", "ideacrafterslabs", "tlc", "feat/foo", "ideacrafterslabs-tlc-feat-foo"},
		{"deep branch", "acme", "svc", "fix/T-0001/sub", "acme-svc-fix-t-0001-sub"},
		{"uppercase normalized", "Acme", "Repo", "Feature/Bar", "acme-repo-feature-bar"},
		{"unsafe chars stripped", "ac me", "re@po", "feat#1", "ac-me-re-po-feat-1"},
		{"missing org falls back to repo", "", "tlc", "main", "tlc-main"},
		{"missing repo falls back to branch", "", "", "main", "main"},
		// Compose requires project names to start with [a-z0-9]. Inputs
		// that would slugify to a leading "_" or "-" must be trimmed.
		{"leading underscore org", "_acme", "svc", "main", "acme-svc-main"},
		{"leading hyphen branch", "acme", "svc", "-main", "acme-svc-main"},
		{"all-underscore segment stripped", "___", "svc", "main", "svc-main"},
		{"all-unsafe input yields empty", "___", "___", "___", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ComposeProjectName(tc.org, tc.repo, tc.branch)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestBuildComposeCommand_InjectsProjectName verifies that the docker compose
// command always carries `-p <project>` so containers, networks, and volumes
// are namespaced per hop. Without this, two hops with the same branch name
// (or stale containers from a prior crashed run) collide on container names
// like `staging-redis-1`. See https://github.com/hop-top/git/issues/12.
func TestBuildComposeCommand_InjectsProjectName(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a compose file so docker.FindComposeFile returns non-empty.
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "docker-compose.yml"), []byte("services: {}\n"), 0644))

	m := &EnvironmentManager{
		Name: "docker-compose",
		Commands: EnvCommands{
			Start: []string{"docker", "compose", "up", "-d"},
			Stop:  []string{"docker", "compose", "stop"},
		},
	}

	t.Run("no override file", func(t *testing.T) {
		got := m.buildComposeCommand(m.Commands.Start, tmpDir, "", "hop-top", "git", "main")
		// Must contain -p hop-top-git-main right after `compose`.
		assert.Equal(t, []string{"docker", "compose", "-p", "hop-top-git-main", "up", "-d"}, got)
	})

	t.Run("with override file", func(t *testing.T) {
		override := filepath.Join(tmpDir, "override.yml")
		require.NoError(t, os.WriteFile(override, []byte("services: {}\n"), 0644))

		got := m.buildComposeCommand(m.Commands.Start, tmpDir, override, "hop-top", "git", "feat/foo")
		// Project name appears, override -f flags appear, original args are preserved.
		assert.Contains(t, got, "-p")
		idx := indexOf(got, "-p")
		require.GreaterOrEqual(t, idx, 0)
		assert.Equal(t, "hop-top-git-feat-foo", got[idx+1])
		assert.Contains(t, got, "-f")
		assert.Contains(t, got, override)
		// up -d still at the end.
		assert.Equal(t, []string{"up", "-d"}, got[len(got)-2:])
	})

	t.Run("non-docker-compose manager untouched", func(t *testing.T) {
		other := &EnvironmentManager{
			Name:     "podman-compose",
			Commands: EnvCommands{Start: []string{"podman-compose", "up", "-d"}},
		}
		got := other.buildComposeCommand(other.Commands.Start, tmpDir, "", "hop-top", "git", "main")
		// Untouched: no project-name injection for non-docker managers.
		assert.Equal(t, []string{"podman-compose", "up", "-d"}, got)
	})
}

func indexOf(s []string, target string) int {
	for i, v := range s {
		if v == target {
			return i
		}
	}
	return -1
}
