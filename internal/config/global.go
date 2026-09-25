package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"hop.top/git/internal/output"
	"hop.top/kit/go/core/xdg"
)

// Additional hop.* config keys not defined in gitconfig.go.
const (
	KeyShellIntegrationPath = "hop.shellIntegration.path"
	KeyShellIntegrationAt   = "hop.shellIntegration.installedAt"

	KeyBackupKeepBackup     = "hop.backup.keepBackup"
	KeyBackupCleanupAgeDays = "hop.backup.cleanupAgeDays"
)

// GlobalLoader handles loading global configuration.
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
// It cannot fail: a managers.json that cannot be read or parsed is named in
// a warning and skipped (built-in managers apply), and the hop.* settings
// from git config still hold.
func (l *GlobalLoader) Load() *GlobalConfig {
	if err := l.maybeMigrate(); err != nil {
		// Migration failure is non-fatal; log and continue
		output.Warn("git-hop config migration failed: %v", err)
	}

	cfg := readFromGitConfig(l.gc)

	// Load complex arrays from managers.json sidecar
	mgrs, err := l.loadManagers()
	if err != nil {
		output.WarnAlways("ignoring %v", managersFileError(err))
		return cfg
	}
	cfg.PackageManagers = mgrs.PackageManagers
	cfg.EnvironmentManagers = mgrs.EnvironmentManagers

	return cfg
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
			EnvAutoStart:      gc.GetBoolOrDefault(KeyEnvAutoStart),
			GitDomain:         gc.GetStringOrDefault(KeyGitDomain),
			WorktreeLocation:  gc.GetStringOrDefault(KeyWorktreeLocation),
			DefaultStartPoint: gc.GetStringOrDefault(KeyAddDefaultStartPoint),
			HooksInstallMode:  gc.GetStringOrDefault(KeyHooksInstallMode),
		},
		ShellIntegration: ShellIntegrationSettings{
			Status:         gc.GetStringOrDefault(KeyShellIntegrationStatus),
			InstalledShell: gc.GetStringOrDefault(KeyShellIntegrationShell),
			InstalledPath:  gc.GetStringOrDefault(KeyShellIntegrationPath),
			InstalledAt:    installedAt,
		},
		Backup: BackupSettings{
			KeepBackup:     gc.GetBoolOrDefault(KeyBackupKeepBackup),
			MaxBackups:     gc.GetIntOrDefault(KeyBackupMaxBackups),
			CleanupAgeDays: gc.GetIntOrDefault(KeyBackupCleanupAgeDays),
		},
	}
}

// WriteShellIntegration persists the shell integration state, and only
// that, to git config --global. There is deliberately no whole-config
// writer: writing every scalar pinned each hop.* default in --global, where
// it shadowed later default changes and read as the user's own choice.
func (l *GlobalLoader) WriteShellIntegration(s ShellIntegrationSettings) error {
	sets := []configEntry{
		{KeyShellIntegrationStatus, s.Status},
		{KeyShellIntegrationShell, s.InstalledShell},
		{KeyShellIntegrationPath, s.InstalledPath},
	}
	if !s.InstalledAt.IsZero() {
		sets = append(sets, configEntry{KeyShellIntegrationAt, s.InstalledAt.Format(time.RFC3339)})
	}
	for _, e := range sets {
		if err := l.gc.Set(e.key, e.val); err != nil {
			return fmt.Errorf("set %s: %w", e.key, err)
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

// ManagersPath is where the managers.json sidecar lives.
func ManagersPath() string { return getManagersPath() }

func getManagersPath() string {
	dir, err := xdg.ConfigDir("git-hop")
	if err != nil {
		return filepath.Join(".config", "git-hop", "managers.json")
	}
	return filepath.Join(dir, "managers.json")
}

// RetiredConfigPath is $XDG_CONFIG_HOME/git-hop/config.json. git-hop no
// longer reads it, or anything else there, for settings: they live in git
// config hop.*. doctor points out a leftover copy; nothing writes or
// removes it.
func RetiredConfigPath() string {
	dir, err := xdg.ConfigDir("git-hop")
	if err != nil {
		return filepath.Join(".config", "git-hop", "config.json")
	}
	return filepath.Join(dir, "config.json")
}

// ManagersFileError reports why managers.json cannot be used, naming the
// file; nil when it is absent or valid.
func (l *GlobalLoader) ManagersFileError() error {
	if _, err := l.loadManagers(); err != nil {
		return managersFileError(err)
	}
	return nil
}

func managersFileError(err error) error {
	return fmt.Errorf("%s: %w", getManagersPath(), err)
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
