package services

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hop.top/git/internal/config"
	"hop.top/git/internal/docker"
)

// ComposeProjectName builds a stable, hop-scoped, compose-safe project name
// from the hub identity and branch. Compose project names must start with an
// alphanumeric character and contain only [a-z0-9_-]. Without a stable
// project name, container/network/volume identifiers fall back to
// filepath.Base(worktreePath), which collides across hubs that share branch
// names and across stale runs of the same hop. See
// https://github.com/hop-top/git/issues/12.
func ComposeProjectName(org, repo, branch string) string {
	parts := []string{}
	if s := composeSlugify(org); s != "" {
		parts = append(parts, s)
	}
	if s := composeSlugify(repo); s != "" {
		parts = append(parts, s)
	}
	if s := composeSlugify(branch); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, "-")
}

// repoIdentity extracts (org, repo) from a HubConfig safely. Returns empty
// strings when the config is nil so callers can fall through to a
// branch-only project name in tests and edge cases.
func repoIdentity(repoConfig *config.HubConfig) (string, string) {
	if repoConfig == nil {
		return "", ""
	}
	return repoConfig.Repo.Org, repoConfig.Repo.Repo
}

// composeSlugify lowercases the input and replaces every run of characters
// outside [a-z0-9_] with a single hyphen, then trims leading separators.
//
// Compose accepts project names matching ^[a-z0-9][a-z0-9_-]*$ — only the
// first character must be alphanumeric; trailing "-" and "_" are legal and
// are preserved here to avoid collisions between distinct inputs (e.g.
// branch "foo" vs branch "foo-" must not slugify to the same name).
// Empty input → empty output. Input consisting entirely of separators
// (e.g. "___") also yields "" because TrimLeft strips the whole string.
func composeSlugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prevHyphen := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
			prevHyphen = false
			continue
		}
		if !prevHyphen {
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	return strings.TrimLeft(b.String(), "-_")
}

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
	org, repo := repoIdentity(repoConfig)
	startCmd := m.buildComposeCommand(m.Commands.Start, worktreePath, overridePath, org, repo, branch)
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
	org, repo := repoIdentity(repoConfig)
	stopCmd := m.buildComposeCommand(m.Commands.Stop, worktreePath, overridePath, org, repo, branch)
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

// buildComposeCommand assembles the docker compose invocation, injecting:
//   - -p <project>  when ComposeProjectName(org, repo, branch) is non-empty
//     (so containers/networks/volumes are hop-scoped). When all slugify
//     inputs reduce to "" the flag is omitted and compose falls back to
//     its default (cwd basename).
//   - -f <composeFile> -f <overridePath> --env-file .env  when an override
//     is provided
//
// For non-docker-compose managers, the command is returned unchanged.
//
// The project-name injection is what makes hops isolated at the
// container/network/volume identifier layer. Without it, two hops with the
// same branch name collide on container names like `staging-redis-1`, and a
// crashed prior run leaves orphaned containers that block restart. See
// https://github.com/hop-top/git/issues/12.
func (m *EnvironmentManager) buildComposeCommand(cmdParts []string, worktreePath, overridePath, org, repo, branch string) []string {
	if m.Name != "docker-compose" {
		return cmdParts
	}
	if len(cmdParts) < 2 {
		return cmdParts
	}

	projectName := ComposeProjectName(org, repo, branch)

	result := make([]string, 0, len(cmdParts)+8)
	result = append(result, cmdParts[0], cmdParts[1]) // "docker", "compose"
	if projectName != "" {
		result = append(result, "-p", projectName)
	}

	if overridePath != "" {
		// Find the compose file in the worktree
		composeFile := docker.FindComposeFile(worktreePath)
		if composeFile != "" {
			result = append(result, "-f", composeFile)
			result = append(result, "-f", overridePath)
			result = append(result, "--env-file", ".env")
		}
	}

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
