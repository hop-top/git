package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"hop.top/kit/xdg"
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
// If a legacy global.json exists and git config is empty,
// migrates values automatically.
func (l *GlobalLoader) Load() (*GlobalConfig, error) {
	if err := l.maybeMigrate(); err != nil {
		// Migration failure is non-fatal; log and continue
		fmt.Fprintf(os.Stderr,
			"warning: git-hop config migration failed: %v\n", err)
	}

	cfg := l.readFromGitConfig()

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

// GetDefaults returns the default global configuration.
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

// readFromGitConfig populates a GlobalConfig from git config hop.* keys.
func (l *GlobalLoader) readFromGitConfig() *GlobalConfig {
	gc := l.gc
	defs := l.GetDefaults()

	installedAt := time.Time{}
	if raw, err := gc.GetString(KeyShellIntegrationAt); err == nil {
		installedAt, _ = time.Parse(time.RFC3339, raw)
	}

	return &GlobalConfig{
		Defaults: DefaultSettings{
			AutoEnvStart:              gc.GetBoolOrDefault(KeyAutoEnvStart),
			ShowAllManagedRepos:       boolOrDefault(gc, KeyShowAllManagedRepos, defs.Defaults.ShowAllManagedRepos),
			UnusedThresholdDays:       intOrDefault(gc, KeyUnusedThresholdDays, defs.Defaults.UnusedThresholdDays),
			BareRepo:                  gc.GetBoolOrDefault(KeyBareRepo),
			EnforceCleanForConversion: boolOrDefault(gc, KeyEnforceCleanForConversion, defs.Defaults.EnforceCleanForConversion),
			ConventionWarning:         gc.GetBoolOrDefault(KeyConventionWarning),
			GitDomain:                 gc.GetStringOrDefault(KeyGitDomain),
			WorktreeLocation:          gc.GetStringOrDefault(KeyWorktreeLocation),
		},
		ShellIntegration: ShellIntegrationSettings{
			Status:         gc.GetStringOrDefault(KeyShellIntegrationStatus),
			InstalledShell: stringOrDefault(gc, KeyShellIntegrationShell, ""),
			InstalledPath:  stringOrDefault(gc, KeyShellIntegrationPath, ""),
			InstalledAt:    installedAt,
		},
		Backup: BackupSettings{
			Enabled:         gc.GetBoolOrDefault(KeyBackupEnabled),
			KeepBackup:      boolOrDefault(gc, KeyBackupKeepBackup, defs.Backup.KeepBackup),
			MaxBackups:      gc.GetIntOrDefault(KeyBackupMaxBackups),
			CleanupAgeDays:  intOrDefault(gc, KeyBackupCleanupAgeDays, defs.Backup.CleanupAgeDays),
			PreserveStashes: boolOrDefault(gc, KeyBackupPreserveStashes, defs.Backup.PreserveStashes),
		},
		Conversion: ConversionSettings{
			EnforceClean:    boolOrDefault(gc, KeyConversionEnforceClean, defs.Conversion.EnforceClean),
			AllowDirtyForce: boolOrDefault(gc, KeyConversionAllowDirtyForce, defs.Conversion.AllowDirtyForce),
			AutoRollback:    boolOrDefault(gc, KeyConversionAutoRollback, defs.Conversion.AutoRollback),
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
		{KeyBareRepo, strconv.FormatBool(cfg.Defaults.BareRepo)},
		{KeyEnforceCleanForConversion, strconv.FormatBool(cfg.Defaults.EnforceCleanForConversion)},
		{KeyConventionWarning, strconv.FormatBool(cfg.Defaults.ConventionWarning)},
		{KeyGitDomain, cfg.Defaults.GitDomain},
		{KeyWorktreeLocation, cfg.Defaults.WorktreeLocation},

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

// maybeMigrate checks if a legacy global.json exists and git config
// hop.* keys are unset, then migrates values to git config --global.
// The JSON file is renamed to global.json.bak (not deleted).
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

	var legacy GlobalConfig
	if err := json.Unmarshal(content, &legacy); err != nil {
		return fmt.Errorf("parse legacy config: %w", err)
	}

	// Write scalar fields to git config
	if err := l.writeToGitConfig(&legacy); err != nil {
		return fmt.Errorf("migrate scalars: %w", err)
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

// --- helpers ---

func boolOrDefault(gc *GitConfig, key string, def bool) bool {
	v, err := gc.GetBool(key)
	if err != nil {
		return def
	}
	return v
}

func intOrDefault(gc *GitConfig, key string, def int) int {
	v, err := gc.GetInt(key)
	if err != nil {
		return def
	}
	return v
}

func stringOrDefault(gc *GitConfig, key string, def string) string {
	v, err := gc.GetString(key)
	if err != nil {
		return def
	}
	return v
}

func getGlobalConfigPath() string {
	dir, err := xdg.ConfigDir("git-hop")
	if err != nil {
		return filepath.Join(".config", "git-hop", "global.json")
	}
	return filepath.Join(dir, "global.json")
}
