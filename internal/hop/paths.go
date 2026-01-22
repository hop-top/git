package hop

import (
	"os"
	"path/filepath"
)

// GetConfigHome returns the XDG config home directory
func GetConfigHome() string {
	if env := os.Getenv("XDG_CONFIG_HOME"); env != "" {
		return env
	}
	return filepath.Join(os.Getenv("HOME"), ".config")
}

// GetDataHome returns the XDG data home directory
func GetDataHome() string {
	if env := os.Getenv("XDG_DATA_HOME"); env != "" {
		return env
	}
	return filepath.Join(os.Getenv("HOME"), ".local", "share")
}

// GetCacheHome returns the XDG cache home directory
func GetCacheHome() string {
	if env := os.Getenv("XDG_CACHE_HOME"); env != "" {
		return env
	}
	return filepath.Join(os.Getenv("HOME"), ".cache")
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
