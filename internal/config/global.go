package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"hop.top/kit/go/core/xdg"
)

// Additional hop.* config keys not defined in gitconfig.go.
const (
	KeyShowAllManagedRepos       = "hop.showAllManagedRepos"
	KeyUnusedThresholdDays       = "hop.unusedThresholdDays"
	KeyEnforceCleanForConversion = "hop.enforceCleanForConversion"

	KeyShellIntegrationPath = "hop.shellIntegration.path"
	KeyShellIntegrationAt   = "hop.shellIntegration.installedAt"

	KeyBackupKeepBackup      = "hop.backup.keepBackup"
	KeyBackupCleanupAgeDays  = "hop.backup.cleanupAgeDays"
	KeyBackupPreserveStashes = "hop.backup.preserveStashes"

	KeyConversionEnforceClean    = "hop.conversion.enforceClean"
	KeyConversionAllowDirtyForce = "hop.conversion.allowDirtyForce"
	KeyConversionAutoRollback    = "hop.conversion.autoRollback"
)

// GlobalLoader handles loading and saving global configuration.
// Reads scalar preferences from git config hop.* keys;
// complex arrays (PackageManagers, EnvironmentManagers) from
// a managers.json sidecar file.
type GlobalLoader struct {
	gc *GitConfig
}

// NewGlobalLoader creates a new global config loader.
func NewGlobalLoader() *GlobalLoader {
	return &GlobalLoader{gc: NewGitConfig()}
}

// NewGlobalLoaderWithGitConfig creates a loader with a custom GitConfig
// (useful for testing).
func NewGlobalLoaderWithGitConfig(gc *GitConfig) *GlobalLoader {
	return &GlobalLoader{gc: gc}
}

// Load reads global config from git config hop.* keys,
// falling back to compiled defaults for missing keys.
// A legacy global.json is migrated first, once (see maybeMigrate).
func (l *GlobalLoader) Load() (*GlobalConfig, error) {
	if err := l.maybeMigrate(); err != nil {
		// Migration failure is non-fatal; log and continue
		fmt.Fprintf(os.Stderr,
			"warning: git-hop config migration failed: %v\n", err)
	}

	cfg := readFromGitConfig(l.gc)

	// Load complex arrays from managers.json sidecar
	mgrs, err := l.loadManagers()
	if err != nil {
		return nil, fmt.Errorf("load managers: %w", err)
	}
	cfg.PackageManagers = mgrs.PackageManagers
	cfg.EnvironmentManagers = mgrs.EnvironmentManagers

	return cfg, nil
}

// Write persists scalar fields to git config --global and
// complex arrays to managers.json.
func (l *GlobalLoader) Write(cfg *GlobalConfig) error {
	if err := l.writeToGitConfig(cfg); err != nil {
		return fmt.Errorf("write git config: %w", err)
	}

	mgrs := managersFile{
		PackageManagers:     cfg.PackageManagers,
		EnvironmentManagers: cfg.EnvironmentManagers,
	}
	if err := l.saveManagers(&mgrs); err != nil {
		return fmt.Errorf("write managers: %w", err)
	}

	return nil
}

// GetDefaults returns the default global configuration: what Load returns
// when git config holds no hop.* keys. It is read through the git-config
// defaults table, so the two cannot drift.
func (l *GlobalLoader) GetDefaults() *GlobalConfig {
	return readFromGitConfig(emptyGitConfig)
}

// emptyGitConfig reports every key as unset.
var emptyGitConfig = &GitConfig{
	RunCmd: func(...string) (string, error) { return "", ErrKeyNotFound },
}

// readFromGitConfig populates a GlobalConfig from git config hop.* keys,
// falling back to the git-config defaults table for unset keys.
func readFromGitConfig(gc *GitConfig) *GlobalConfig {
	installedAt := time.Time{}
	if raw, err := gc.GetString(KeyShellIntegrationAt); err == nil {
		installedAt, _ = time.Parse(time.RFC3339, raw)
	}

	return &GlobalConfig{
		Defaults: DefaultSettings{
			AutoEnvStart:              gc.GetBoolOrDefault(KeyAutoEnvStart),
			ShowAllManagedRepos:       gc.GetBoolOrDefault(KeyShowAllManagedRepos),
			UnusedThresholdDays:       gc.GetIntOrDefault(KeyUnusedThresholdDays),
			EnforceCleanForConversion: gc.GetBoolOrDefault(KeyEnforceCleanForConversion),
			ConventionWarning:         gc.GetBoolOrDefault(KeyConventionWarning),
			GitDomain:                 gc.GetStringOrDefault(KeyGitDomain),
			WorktreeLocation:          gc.GetStringOrDefault(KeyWorktreeLocation),
			DefaultStartPoint:         gc.GetStringOrDefault(KeyAddDefaultStartPoint),
			HooksInstallMode:          gc.GetStringOrDefault(KeyHooksInstallMode),
		},
		ShellIntegration: ShellIntegrationSettings{
			Status:         gc.GetStringOrDefault(KeyShellIntegrationStatus),
			InstalledShell: gc.GetStringOrDefault(KeyShellIntegrationShell),
			InstalledPath:  gc.GetStringOrDefault(KeyShellIntegrationPath),
			InstalledAt:    installedAt,
		},
		Backup: BackupSettings{
			Enabled:         gc.GetBoolOrDefault(KeyBackupEnabled),
			KeepBackup:      gc.GetBoolOrDefault(KeyBackupKeepBackup),
			MaxBackups:      gc.GetIntOrDefault(KeyBackupMaxBackups),
			CleanupAgeDays:  gc.GetIntOrDefault(KeyBackupCleanupAgeDays),
			PreserveStashes: gc.GetBoolOrDefault(KeyBackupPreserveStashes),
		},
		Conversion: ConversionSettings{
			EnforceClean:    gc.GetBoolOrDefault(KeyConversionEnforceClean),
			AllowDirtyForce: gc.GetBoolOrDefault(KeyConversionAllowDirtyForce),
			AutoRollback:    gc.GetBoolOrDefault(KeyConversionAutoRollback),
		},
	}
}

// writeToGitConfig persists all scalar fields to git config --global.
func (l *GlobalLoader) writeToGitConfig(cfg *GlobalConfig) error {
	gc := l.gc
	sets := []struct {
		key string
		val string
	}{
		{KeyAutoEnvStart, strconv.FormatBool(cfg.Defaults.AutoEnvStart)},
		{KeyShowAllManagedRepos, strconv.FormatBool(cfg.Defaults.ShowAllManagedRepos)},
		{KeyUnusedThresholdDays, strconv.Itoa(cfg.Defaults.UnusedThresholdDays)},
		{KeyEnforceCleanForConversion, strconv.FormatBool(cfg.Defaults.EnforceCleanForConversion)},
		{KeyConventionWarning, strconv.FormatBool(cfg.Defaults.ConventionWarning)},
		{KeyGitDomain, cfg.Defaults.GitDomain},
		{KeyWorktreeLocation, cfg.Defaults.WorktreeLocation},
		{KeyAddDefaultStartPoint, cfg.Defaults.DefaultStartPoint},
		{KeyHooksInstallMode, cfg.Defaults.HooksInstallMode},

		{KeyShellIntegrationStatus, cfg.ShellIntegration.Status},
		{KeyShellIntegrationShell, cfg.ShellIntegration.InstalledShell},
		{KeyShellIntegrationPath, cfg.ShellIntegration.InstalledPath},

		{KeyBackupEnabled, strconv.FormatBool(cfg.Backup.Enabled)},
		{KeyBackupKeepBackup, strconv.FormatBool(cfg.Backup.KeepBackup)},
		{KeyBackupMaxBackups, strconv.Itoa(cfg.Backup.MaxBackups)},
		{KeyBackupCleanupAgeDays, strconv.Itoa(cfg.Backup.CleanupAgeDays)},
		{KeyBackupPreserveStashes, strconv.FormatBool(cfg.Backup.PreserveStashes)},

		{KeyConversionEnforceClean, strconv.FormatBool(cfg.Conversion.EnforceClean)},
		{KeyConversionAllowDirtyForce, strconv.FormatBool(cfg.Conversion.AllowDirtyForce)},
		{KeyConversionAutoRollback, strconv.FormatBool(cfg.Conversion.AutoRollback)},
	}

	// Write installedAt only if non-zero
	if !cfg.ShellIntegration.InstalledAt.IsZero() {
		sets = append(sets, struct {
			key string
			val string
		}{KeyShellIntegrationAt, cfg.ShellIntegration.InstalledAt.Format(time.RFC3339)})
	}

	for _, s := range sets {
		if err := gc.Set(s.key, s.val); err != nil {
			return fmt.Errorf("set %s: %w", s.key, err)
		}
	}
	return nil
}

// --- Migration from legacy global.json ---

// maybeMigrate migrates a legacy global.json to git config --global unless
// the hop.migrated sentinel is set. Only keys present in the file are
// written. The JSON file is renamed to global.json.bak (not deleted).
func (l *GlobalLoader) maybeMigrate() error {
	jsonPath := getGlobalConfigPath()
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		return nil // nothing to migrate
	}

	// Check if already migrated via sentinel key
	if v, err := l.gc.GetString("hop.migrated"); err == nil && v == "true" {
		return nil
	}

	content, err := os.ReadFile(jsonPath)
	if err != nil {
		return fmt.Errorf("read legacy config: %w", err)
	}

	var legacy legacyGlobalConfig
	if err := json.Unmarshal(content, &legacy); err != nil {
		return fmt.Errorf("parse legacy config: %w", err)
	}

	for _, e := range legacy.gitConfigEntries() {
		if err := l.gc.Set(e.key, e.val); err != nil {
			return fmt.Errorf("migrate scalars: set %s: %w", e.key, err)
		}
	}

	// Extract managers to sidecar file
	if len(legacy.PackageManagers) > 0 || len(legacy.EnvironmentManagers) > 0 {
		mgrs := managersFile{
			PackageManagers:     legacy.PackageManagers,
			EnvironmentManagers: legacy.EnvironmentManagers,
		}
		if err := l.saveManagers(&mgrs); err != nil {
			return fmt.Errorf("migrate managers: %w", err)
		}
	}

	// Mark migration complete
	if err := l.gc.Set("hop.migrated", "true"); err != nil {
		return fmt.Errorf("set migration sentinel: %w", err)
	}

	// Backup original JSON (rename, not delete)
	bakPath := jsonPath + ".bak"
	if _, err := os.Stat(bakPath); err == nil {
		if err := os.Remove(bakPath); err != nil {
			return fmt.Errorf("remove existing legacy backup: %w", err)
		}
	}
	if err := os.Rename(jsonPath, bakPath); err != nil {
		return fmt.Errorf("backup legacy config: %w", err)
	}

	return nil
}

// --- managers.json sidecar ---

type managersFile struct {
	PackageManagers     []PackageManagerConfig `json:"packageManagers,omitempty"`
	EnvironmentManagers []EnvManagerConfig     `json:"environmentManagers,omitempty"`
}

func getManagersPath() string {
	dir, err := xdg.ConfigDir("git-hop")
	if err != nil {
		return filepath.Join(".config", "git-hop", "managers.json")
	}
	return filepath.Join(dir, "managers.json")
}

func (l *GlobalLoader) loadManagers() (*managersFile, error) {
	path := getManagersPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &managersFile{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var mgrs managersFile
	if err := json.Unmarshal(data, &mgrs); err != nil {
		return nil, err
	}
	return &mgrs, nil
}

func (l *GlobalLoader) saveManagers(mgrs *managersFile) error {
	if len(mgrs.PackageManagers) == 0 && len(mgrs.EnvironmentManagers) == 0 {
		return nil // nothing to write
	}

	path := getManagersPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(mgrs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func getGlobalConfigPath() string {
	dir, err := xdg.ConfigDir("git-hop")
	if err != nil {
		return filepath.Join(".config", "git-hop", "global.json")
	}
	return filepath.Join(dir, "global.json")
}
