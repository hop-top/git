package hop

import (
	"os"
	"path/filepath"

	"hop.top/kit/xdg"
)

const toolName = "git-hop"

// GetConfigHome returns the XDG config home directory (base, without tool suffix).
func GetConfigHome() string {
	dir, err := xdg.ConfigDir(toolName)
	if err != nil {
		return ".config"
	}
	return filepath.Dir(dir)
}

// GetDataHome returns the XDG data home directory (base, without tool suffix).
func GetDataHome() string {
	dir, err := xdg.DataDir(toolName)
	if err != nil {
		return filepath.Join(".local", "share")
	}
	return filepath.Dir(dir)
}

// GetCacheHome returns the XDG cache home directory (base, without tool suffix).
func GetCacheHome() string {
	dir, err := xdg.CacheDir(toolName)
	if err != nil {
		return ".cache"
	}
	return filepath.Dir(dir)
}

func configDir() string {
	dir, err := xdg.ConfigDir(toolName)
	if err != nil {
		return filepath.Join(".config", toolName)
	}
	return dir
}

func cacheDir() string {
	dir, err := xdg.CacheDir(toolName)
	if err != nil {
		return filepath.Join(".cache", toolName)
	}
	return dir
}

func dataDir() string {
	dir, err := xdg.DataDir(toolName)
	if err != nil {
		return filepath.Join(".local", "share", toolName)
	}
	return dir
}

func stateDir() string {
	dir, err := xdg.StateDir(toolName)
	if err != nil {
		return filepath.Join(".local", "state", toolName)
	}
	return dir
}

func GetHopsRegistryPath() string {
	return filepath.Join(configDir(), "hops.json")
}

func GetGlobalConfigPath() string {
	return filepath.Join(configDir(), "global.json")
}

func GetHooksDir() string {
	return filepath.Join(configDir(), "hooks")
}

func GetBackupBasePath() string {
	return cacheDir()
}

func GetGitHopCacheHome() string {
	return cacheDir()
}

func GetComposeOverrideCachePath(org, repo, branch string) string {
	return filepath.Join(cacheDir(), org, repo, branch, "docker-compose.override.yml")
}

func GetOverrideMetaCachePath(org, repo, branch string) string {
	return filepath.Join(cacheDir(), org, repo, branch, ".override-meta.json")
}

func GetGitHopDataHome() string {
	if env := os.Getenv("GIT_HOP_DATA_HOME"); env != "" {
		return env
	}
	return dataDir()
}

func GetStateHome() string {
	return stateDir()
}
