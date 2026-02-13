package services

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/docker"
)

// EnvironmentManager represents an environment manager (docker-compose, podman, etc.)
type EnvironmentManager struct {
	Name        string
	DetectFiles []string
	Commands    EnvCommands
	Hooks       EnvHooks
}

// EnvCommands defines lifecycle commands for an environment manager
type EnvCommands struct {
	Start   []string
	Stop    []string
	Health  []string
	Restart []string
	Logs    []string
}

// EnvHooks defines lifecycle hooks for an environment manager
type EnvHooks struct {
	PreStart  []string
	PostStart []string
	PreStop   []string
	PostStop  []string
}

// LoadEnvManagers loads built-in and custom environment managers from config
func LoadEnvManagers(globalConfig *config.GlobalConfig) ([]EnvironmentManager, error) {
	managers := []EnvironmentManager{}

	// Add built-in docker-compose manager
	managers = append(managers, EnvironmentManager{
		Name:        "docker-compose",
		DetectFiles: []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"},
		Commands: EnvCommands{
			Start:  []string{"docker", "compose", "up", "-d"},
			Stop:   []string{"docker", "compose", "stop"},
			Health: []string{"docker", "compose", "ps", "--format", "json"},
		},
	})

	// Add custom managers from global config
	if globalConfig != nil {
		for _, cfg := range globalConfig.EnvironmentManagers {
			managers = append(managers, EnvironmentManager{
				Name:        cfg.Name,
				DetectFiles: cfg.DetectFiles,
				Commands: EnvCommands{
					Start:   cfg.Commands.Start,
					Stop:    cfg.Commands.Stop,
					Health:  cfg.Commands.Health,
					Restart: cfg.Commands.Restart,
					Logs:    cfg.Commands.Logs,
				},
				Hooks: EnvHooks{
					PreStart:  cfg.Hooks.PreStart,
					PostStart: cfg.Hooks.PostStart,
					PreStop:   cfg.Hooks.PreStop,
					PostStop:  cfg.Hooks.PostStop,
				},
			})
		}
	}

	return managers, nil
}

// DetectEnvManager detects which manager to use for a worktree
func DetectEnvManager(worktreePath string, repoConfig *config.HubConfig, availableManagers []EnvironmentManager) (*EnvironmentManager, error) {
	// Check for explicit override in repo config
	if repoConfig != nil && repoConfig.Settings.EnvironmentManager != nil {
		managerName := *repoConfig.Settings.EnvironmentManager

		// Special case: "none" means explicitly no manager
		if managerName == "none" {
			return nil, nil
		}

		// Find manager by name
		for i := range availableManagers {
			if availableManagers[i].Name == managerName {
				return &availableManagers[i], nil
			}
		}

		return nil, fmt.Errorf("configured environment manager '%s' not found", managerName)
	}

	// Auto-detect based on detect files
	for i := range availableManagers {
		manager := &availableManagers[i]
		for _, detectFile := range manager.DetectFiles {
			fullPath := filepath.Join(worktreePath, detectFile)
			if _, err := os.Stat(fullPath); err == nil {
				return manager, nil
			}
		}
	}

	// No manager detected
	return nil, nil
}

// Start starts the environment using this manager.
// If overridePath is non-empty, it is passed as an additional -f flag to docker compose.
func (m *EnvironmentManager) Start(worktreePath, branch, repoPath string, repoConfig *config.HubConfig, overridePath string) error {
	ctx := HookContext{
		WorktreePath: worktreePath,
		Branch:       branch,
		RepoPath:     repoPath,
		Command:      "start",
	}

	// Merge hooks: global hooks first, then per-repo hooks
	allPreStartHooks := append([]string{}, m.Hooks.PreStart...)
	allPostStartHooks := append([]string{}, m.Hooks.PostStart...)

	if repoConfig != nil && repoConfig.Settings.EnvironmentConfig != nil {
		allPreStartHooks = append(allPreStartHooks, repoConfig.Settings.EnvironmentConfig.Hooks.PreStart...)
		allPostStartHooks = append(allPostStartHooks, repoConfig.Settings.EnvironmentConfig.Hooks.PostStart...)
	}

	// Execute preStart hooks
	if len(allPreStartHooks) > 0 {
		fmt.Printf("  → Running preStart hooks...\n")
		if err := ExecuteHooksWithTimeout(allPreStartHooks, ctx, 5*time.Minute); err != nil {
			return fmt.Errorf("preStart hook failed: %w", err)
		}
	}

	// Execute start command
	fmt.Printf("  → Starting services: %s\n", m.Name)
	startCmd := m.buildCommandWithOverride(m.Commands.Start, worktreePath, overridePath)
	if err := m.executeCommand(startCmd, worktreePath); err != nil {
		return fmt.Errorf("start command failed: %w", err)
	}

	// Execute postStart hooks
	if len(allPostStartHooks) > 0 {
		fmt.Printf("  → Running postStart hooks...\n")
		if err := ExecuteHooksWithTimeout(allPostStartHooks, ctx, 5*time.Minute); err != nil {
			return fmt.Errorf("postStart hook failed: %w", err)
		}
	}

	fmt.Printf("  ✓ Environment started successfully\n")
	return nil
}

// Stop stops the environment using this manager.
// If overridePath is non-empty, it is passed as an additional -f flag to docker compose.
func (m *EnvironmentManager) Stop(worktreePath, branch, repoPath string, repoConfig *config.HubConfig, overridePath string) error {
	ctx := HookContext{
		WorktreePath: worktreePath,
		Branch:       branch,
		RepoPath:     repoPath,
		Command:      "stop",
	}

	// Merge hooks: global hooks first, then per-repo hooks
	allPreStopHooks := append([]string{}, m.Hooks.PreStop...)
	allPostStopHooks := append([]string{}, m.Hooks.PostStop...)

	if repoConfig != nil && repoConfig.Settings.EnvironmentConfig != nil {
		allPreStopHooks = append(allPreStopHooks, repoConfig.Settings.EnvironmentConfig.Hooks.PreStop...)
		allPostStopHooks = append(allPostStopHooks, repoConfig.Settings.EnvironmentConfig.Hooks.PostStop...)
	}

	// Execute preStop hooks
	if len(allPreStopHooks) > 0 {
		fmt.Printf("  → Running preStop hooks...\n")
		if err := ExecuteHooksWithTimeout(allPreStopHooks, ctx, 5*time.Minute); err != nil {
			return fmt.Errorf("preStop hook failed: %w", err)
		}
	}

	// Execute stop command
	fmt.Printf("  → Stopping services: %s\n", m.Name)
	stopCmd := m.buildCommandWithOverride(m.Commands.Stop, worktreePath, overridePath)
	if err := m.executeCommand(stopCmd, worktreePath); err != nil {
		return fmt.Errorf("stop command failed: %w", err)
	}

	// Execute postStop hooks
	if len(allPostStopHooks) > 0 {
		fmt.Printf("  → Running postStop hooks...\n")
		if err := ExecuteHooksWithTimeout(allPostStopHooks, ctx, 5*time.Minute); err != nil {
			return fmt.Errorf("postStop hook failed: %w", err)
		}
	}

	fmt.Printf("  ✓ Environment stopped successfully\n")
	return nil
}

// Health checks if environment is running
func (m *EnvironmentManager) Health(worktreePath string) (bool, error) {
	if len(m.Commands.Health) == 0 {
		// No health check defined
		return false, nil
	}

	cmd := exec.Command(m.Commands.Health[0], m.Commands.Health[1:]...)
	cmd.Dir = worktreePath

	err := cmd.Run()
	if err != nil {
		// Health check failed (services not running or unhealthy)
		return false, nil
	}

	return true, nil
}

// executeCommand executes a command in the worktree directory
func (m *EnvironmentManager) executeCommand(cmdParts []string, worktreePath string) error {
	if len(cmdParts) == 0 {
		return fmt.Errorf("empty command")
	}

	cmd := exec.Command(cmdParts[0], cmdParts[1:]...)
	cmd.Dir = worktreePath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// buildCommandWithOverride injects -f flags for compose override files into docker compose commands.
// For non-docker-compose managers or when overridePath is empty, returns the original command unchanged.
func (m *EnvironmentManager) buildCommandWithOverride(cmdParts []string, worktreePath, overridePath string) []string {
	if overridePath == "" || m.Name != "docker-compose" {
		return cmdParts
	}

	// Find the compose file in the worktree
	composeFile := docker.FindComposeFile(worktreePath)
	if composeFile == "" {
		return cmdParts
	}

	// Build: docker compose -f <composeFile> -f <overridePath> --env-file .env <rest...>
	// The original command is like: ["docker", "compose", "up", "-d"]
	// We inject -f flags after "compose"
	if len(cmdParts) < 2 {
		return cmdParts
	}

	result := make([]string, 0, len(cmdParts)+6)
	result = append(result, cmdParts[0], cmdParts[1]) // "docker", "compose"
	result = append(result, "-f", composeFile)
	result = append(result, "-f", overridePath)
	result = append(result, "--env-file", ".env")
	result = append(result, cmdParts[2:]...) // "up", "-d" or "stop" etc.
	return result
}

// Validate checks that the manager configuration is valid
func (m *EnvironmentManager) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("manager name is required")
	}
	if len(m.DetectFiles) == 0 {
		return fmt.Errorf("manager %s: at least one detect file is required", m.Name)
	}
	if len(m.Commands.Start) == 0 {
		return fmt.Errorf("manager %s: start command is required", m.Name)
	}
	if len(m.Commands.Stop) == 0 {
		return fmt.Errorf("manager %s: stop command is required", m.Name)
	}
	return nil
}
