package config

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// GitConfig provides typed accessors for hop.* keys stored in git config.
// This is the canonical way to read/write user preferences — git config
// is the native store for git plugins (like git-lfs [lfs], git-flow [gitflow]).
type GitConfig struct {
	// RunCmd executes a command and returns trimmed stdout.
	// Defaults to execGitConfig; override for testing.
	RunCmd func(args ...string) (string, error)
}

// Known hop.* config keys with their defaults.
const (
	KeyGitDomain              = "hop.gitDomain"
	KeyEnvAutoStart           = "hop.env.autoStart"
	KeyWorktreeLocation       = "hop.worktreeLocation"
	KeyAddDefaultStartPoint   = "hop.add.defaultStartPoint"
	KeyAddCopyIgnored         = "hop.add.copyIgnored"
	KeyAddCopyIgnoredMaxSize  = "hop.add.copyIgnoredMaxSize"
	KeyAddFetch               = "hop.add.fetch"
	KeyMergeDeleteRemote      = "hop.merge.deleteRemote"
	KeyShellIntegrationStatus = "hop.shellIntegration.status"
	KeyShellIntegrationShell  = "hop.shellIntegration.shell"
	KeyBackupMaxBackups       = "hop.backup.maxBackups"
	KeyBackupPath             = "hop.backup.path"
	KeyHooksInstallMode       = "hop.hooks.installMode"
	KeyEventsSink             = "hop.events.sink"
	KeyEventsPath             = "hop.events.path"
	KeyGitflowEnabled         = "hop.gitflow.enabled"
)

// defaults is the single source of compiled defaults for hop.* keys.
// Every *OrDefault accessor falls back to it, and GlobalLoader.GetDefaults
// is derived from it. A key missing here defaults to its zero value.
var defaults = map[string]string{
	KeyGitDomain:              "github.com",
	KeyEnvAutoStart:           "false",
	KeyWorktreeLocation:       "{hubPath}/hops/{branch}",
	KeyAddDefaultStartPoint:   "default-branch",
	KeyAddCopyIgnored:         "true",
	KeyAddCopyIgnoredMaxSize:  "10m",
	KeyShellIntegrationStatus: "unknown",
	KeyBackupKeepBackup:       "false",
	KeyBackupMaxBackups:       "3",
	KeyBackupCleanupAgeDays:   "30",
	KeyHooksInstallMode:       "prompt",
	KeyGitflowEnabled:         "false",
}

// NewGitConfig returns a GitConfig that shells out to git.
func NewGitConfig() *GitConfig {
	return &GitConfig{RunCmd: execGitConfig}
}

// NewGitConfigIn returns a GitConfig that reads the repository at dir
// (`git -C <dir> config`) rather than the one around the process's cwd.
// Use it for per-repo settings read alongside other config of that repo.
func NewGitConfigIn(dir string) *GitConfig {
	return &GitConfig{RunCmd: func(args ...string) (string, error) {
		return execGitConfig(append([]string{"-C", dir}, args...)...)
	}}
}

// NewGlobalGitConfig returns a GitConfig that reads --global scope only,
// whatever repository the process runs in. Use it for a setting that
// belongs to a repository no longer on disk.
func NewGlobalGitConfig() *GitConfig {
	return &GitConfig{RunCmd: func(args ...string) (string, error) {
		if len(args) > 0 && args[0] == "config" {
			args = append([]string{"config", "--global"}, args[1:]...)
		}
		return execGitConfig(args...)
	}}
}

// GetBool reads a boolean from git config.
// Missing keys return (false, ErrKeyNotFound).
func (gc *GitConfig) GetBool(key string) (bool, error) {
	raw, err := gc.get(key)
	if err != nil {
		return false, err
	}
	return parseBool(raw)
}

// GetString reads a string from git config.
// Missing keys return ("", ErrKeyNotFound).
func (gc *GitConfig) GetString(key string) (string, error) {
	return gc.get(key)
}

// GetInt reads an integer from git config.
// Missing keys return (0, ErrKeyNotFound).
func (gc *GitConfig) GetInt(key string) (int, error) {
	raw, err := gc.get(key)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(raw)
}

// GetPath reads a path from git config with git's own path conversion
// (`--type=path`), so `~/` and `~user/` expand the way git expands them.
// Missing keys return ("", ErrKeyNotFound).
func (gc *GitConfig) GetPath(key string) (string, error) {
	return gc.get(key, "--type=path")
}

// Set writes a key to --global scope.
func (gc *GitConfig) Set(key, value string) error {
	_, err := gc.RunCmd("config", "--global", key, value)
	return err
}

// GetGlobal reads a key from --global scope only.
// Missing keys return ("", ErrKeyNotFound).
func (gc *GitConfig) GetGlobal(key string) (string, error) {
	return gc.get(key, "--global")
}

// UnsetGlobal removes a key from --global scope.
func (gc *GitConfig) UnsetGlobal(key string) error {
	_, err := gc.RunCmd("config", "--global", "--unset", key)
	return err
}

// SetLocal writes a key to --local scope.
func (gc *GitConfig) SetLocal(key, value string) error {
	_, err := gc.RunCmd("config", "--local", key, value)
	return err
}

// ErrKeyNotFound is returned when a git config key does not exist.
var ErrKeyNotFound = fmt.Errorf("git config key not found")

// get retrieves a raw value via `git config --get`.
func (gc *GitConfig) get(key string, opts ...string) (string, error) {
	args := append(append([]string{"config"}, opts...), "--get", key)
	out, err := gc.RunCmd(args...)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "", ErrKeyNotFound
		}
		return "", fmt.Errorf("git config --get %s: %w", key, err)
	}
	return out, nil
}

// execGitConfig runs `git <args>` and returns trimmed stdout.
func execGitConfig(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// parseBool handles git's bool representations: true/false/yes/no/on/off/1/0.
func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "yes", "on", "1":
		return true, nil
	case "false", "no", "off", "0":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value: %q", s)
	}
}

// ParseSize parses a byte count that may carry one of git's own size
// suffixes: k/K (KiB), m/M (MiB), g/G (GiB), as accepted by numeric config
// values like core.bigFileThreshold. A bare number is bytes. Negative
// values are rejected — the callers use the result as a ceiling, and a
// negative ceiling would silently disable the guard it configures.
func ParseSize(s string) (int64, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return 0, fmt.Errorf("empty size value")
	}

	mult := int64(1)
	switch raw[len(raw)-1] {
	case 'k', 'K':
		mult = 1 << 10
	case 'm', 'M':
		mult = 1 << 20
	case 'g', 'G':
		mult = 1 << 30
	}
	if mult != 1 {
		raw = strings.TrimSpace(raw[:len(raw)-1])
	}

	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size value: %q", s)
	}
	if n < 0 {
		return 0, fmt.Errorf("size value must not be negative: %q", s)
	}
	return n * mult, nil
}

// GetSizeOrDefault returns the byte count for key, falling back to the
// compiled default if the key is missing or unparseable. An unparseable
// user value falls back rather than failing: a typo'd size must not break
// `git hop add`.
func (gc *GitConfig) GetSizeOrDefault(key string) int64 {
	if raw, err := gc.GetString(key); err == nil {
		if n, err := ParseSize(raw); err == nil {
			return n
		}
	}
	if d, ok := defaults[key]; ok {
		n, _ := ParseSize(d)
		return n
	}
	return 0
}

// GetBoolOrDefault returns the value for key, falling back to the
// compiled default if the key is missing.
func (gc *GitConfig) GetBoolOrDefault(key string) bool {
	v, err := gc.GetBool(key)
	if err != nil {
		if d, ok := defaults[key]; ok {
			b, _ := parseBool(d)
			return b
		}
		return false
	}
	return v
}

// GetStringOrDefault returns the value for key, falling back to the
// compiled default if the key is missing.
func (gc *GitConfig) GetStringOrDefault(key string) string {
	v, err := gc.GetString(key)
	if err != nil {
		if d, ok := defaults[key]; ok {
			return d
		}
		return ""
	}
	return v
}

// GetIntOrDefault returns the value for key, falling back to the
// compiled default if the key is missing.
func (gc *GitConfig) GetIntOrDefault(key string) int {
	v, err := gc.GetInt(key)
	if err != nil {
		if d, ok := defaults[key]; ok {
			n, _ := strconv.Atoi(d)
			return n
		}
		return 0
	}
	return v
}
