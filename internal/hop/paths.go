package hop

import (
	"os"
	"path/filepath"
	"runtime"
)

// GetConfigHome returns the XDG config home directory
func GetConfigHome() string {
	if env := os.Getenv("XDG_CONFIG_HOME"); env != "" {
		return env
	}

	// Use os.UserConfigDir() which handles OS-specific defaults
	if configDir, err := os.UserConfigDir(); err == nil {
		return configDir
	}

	// Fallback to $HOME/.config
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, ".config")
	}

	return ".config"
}

// GetDataHome returns the XDG data home directory
func GetDataHome() string {
	if env := os.Getenv("XDG_DATA_HOME"); env != "" {
		return env
	}

	// Use OS-specific defaults
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}

	if home == "" {
		return ".local/share"
	}

	switch runtime.GOOS {
	case "windows":
		// Windows: %LOCALAPPDATA%
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return localAppData
		}
		return filepath.Join(home, "AppData", "Local")
	case "darwin":
		// macOS: ~/Library/Application Support
		return filepath.Join(home, "Library", "Application Support")
	default:
		// Linux/Unix: ~/.local/share
		return filepath.Join(home, ".local", "share")
	}
}

// GetCacheHome returns the XDG cache home directory
func GetCacheHome() string {
	if env := os.Getenv("XDG_CACHE_HOME"); env != "" {
		return env
	}

	// Use os.UserCacheDir() which handles OS-specific defaults
	if cacheDir, err := os.UserCacheDir(); err == nil {
		return cacheDir
	}

	// Fallback
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, ".cache")
	}

	return ".cache"
}

// GetHopsRegistryPath returns the path to the hops registry file
func GetHopsRegistryPath() string {
	return filepath.Join(GetConfigHome(), "git-hop", "hops.json")
}

// GetGlobalConfigPath returns the path to the global config file
func GetGlobalConfigPath() string {
	return filepath.Join(GetConfigHome(), "git-hop", "global.json")
}

// GetHooksDir returns the directory for shell hooks
func GetHooksDir() string {
	return filepath.Join(GetConfigHome(), "git-hop", "hooks")
}

// GetBackupBasePath returns the base directory for backups
func GetBackupBasePath() string {
	return filepath.Join(GetCacheHome(), "git-hop")
}

// GetGitHopDataHome returns the git-hop data directory
// Checks GIT_HOP_DATA_HOME first, then falls back to OS-appropriate defaults
func GetGitHopDataHome() string {
	if env := os.Getenv("GIT_HOP_DATA_HOME"); env != "" {
		return env
	}
	return filepath.Join(GetDataHome(), "git-hop")
}

// GetStateHome returns the XDG state home directory
func GetStateHome() string {
	if env := os.Getenv("XDG_STATE_HOME"); env != "" {
		return env
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}

	if home == "" {
		return ".local/state"
	}

	switch runtime.GOOS {
	case "darwin":
		// macOS: ~/Library/Application Support/state
		return filepath.Join(home, "Library", "Application Support", "state")
	default:
		// Linux/Unix: ~/.local/state
		return filepath.Join(home, ".local", "state")
	}
}
