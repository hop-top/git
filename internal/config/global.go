package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"hop.top/kit/xdg"
)

// GlobalLoader handles loading and saving global configuration
type GlobalLoader struct{}

// NewGlobalLoader creates a new global config loader
func NewGlobalLoader() *GlobalLoader {
	return &GlobalLoader{}
}

// Load reads the global config file or returns defaults
func (l *GlobalLoader) Load() (*GlobalConfig, error) {
	path := getGlobalConfigPath()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return l.GetDefaults(), nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg GlobalConfig
	if err := json.Unmarshal(content, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Write saves the global config to file
func (l *GlobalLoader) Write(cfg *GlobalConfig) error {
	path := getGlobalConfigPath()
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	content, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, content, 0644)
}

// GetDefaults returns the default global configuration
func (l *GlobalLoader) GetDefaults() *GlobalConfig {
	return &GlobalConfig{
		Defaults: DefaultSettings{
			AutoEnvStart:              true,
			ShowAllManagedRepos:       false,
			UnusedThresholdDays:       30,
			BareRepo:                  true,
			EnforceCleanForConversion: true,
			ConventionWarning:         true,
			WorktreeLocation:          "{hubPath}/hops/{branch}",
		},
		ShellIntegration: ShellIntegrationSettings{
			Status: "unknown",
		},
		Backup: BackupSettings{
			Enabled:         true,
			KeepBackup:      false,
			MaxBackups:      3,
			CleanupAgeDays:  30,
			PreserveStashes: true,
		},
		Conversion: ConversionSettings{
			EnforceClean:    true,
			AllowDirtyForce: false,
			AutoRollback:    true,
		},
	}
}

func getGlobalConfigPath() string {
	dir, err := xdg.ConfigDir("git-hop")
	if err != nil {
		return filepath.Join(".config", "git-hop", "global.json")
	}
	return filepath.Join(dir, "global.json")
}
