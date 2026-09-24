package config

import (
	"strconv"
	"time"
)

// legacyGlobalConfig mirrors the global.json schema with pointer scalars so
// migration can tell a key the user wrote from one the file never had.
// Decoding into GlobalConfig would turn every absent key into false/0/"",
// and writing those to git config would shadow the compiled defaults.
type legacyGlobalConfig struct {
	Defaults struct {
		AutoEnvStart              *bool   `json:"autoEnvStart"`
		ShowAllManagedRepos       *bool   `json:"showAllManagedRepos"`
		UnusedThresholdDays       *int    `json:"unusedThresholdDays"`
		EnforceCleanForConversion *bool   `json:"enforceCleanForConversion"`
		ConventionWarning         *bool   `json:"conventionWarning"`
		GitDomain                 *string `json:"gitDomain"`
		WorktreeLocation          *string `json:"worktreeLocation"`
		DefaultStartPoint         *string `json:"defaultStartPoint"`
		HooksInstallMode          *string `json:"hooksInstallMode"`
	} `json:"defaults"`
	ShellIntegration struct {
		Status         *string    `json:"status"`
		InstalledShell *string    `json:"installedShell"`
		InstalledPath  *string    `json:"installedPath"`
		InstalledAt    *time.Time `json:"installedAt"`
	} `json:"shellIntegration"`
	PackageManagers     []PackageManagerConfig `json:"packageManagers"`
	EnvironmentManagers []EnvManagerConfig     `json:"environmentManagers"`
	Backup              struct {
		Enabled         *bool `json:"enabled"`
		KeepBackup      *bool `json:"keepBackup"`
		MaxBackups      *int  `json:"maxBackups"`
		CleanupAgeDays  *int  `json:"cleanupAgeDays"`
		PreserveStashes *bool `json:"preserveStashes"`
	} `json:"backup"`
	Conversion struct {
		EnforceClean    *bool `json:"enforceClean"`
		AllowDirtyForce *bool `json:"allowDirtyForce"`
		AutoRollback    *bool `json:"autoRollback"`
	} `json:"conversion"`
}

type configEntry struct {
	key string
	val string
}

// gitConfigEntries returns the hop.* entries for the keys present in the
// legacy file.
//
// An empty string is skipped for every key but worktreeLocation: legacy
// git-hop wrote "gitDomain": "" by default and read "" as github.com, and no
// other string key gives "" a meaning, so writing it would only shadow the
// default. worktreeLocation "" selects the centralized layout and is kept.
func (lc *legacyGlobalConfig) gitConfigEntries() []configEntry {
	var out []configEntry
	addBool := func(key string, v *bool) {
		if v != nil {
			out = append(out, configEntry{key, strconv.FormatBool(*v)})
		}
	}
	addInt := func(key string, v *int) {
		if v != nil {
			out = append(out, configEntry{key, strconv.Itoa(*v)})
		}
	}
	addString := func(key string, v *string) {
		if v != nil && *v != "" {
			out = append(out, configEntry{key, *v})
		}
	}

	d := &lc.Defaults
	addBool(KeyAutoEnvStart, d.AutoEnvStart)
	addBool(KeyShowAllManagedRepos, d.ShowAllManagedRepos)
	addInt(KeyUnusedThresholdDays, d.UnusedThresholdDays)
	addBool(KeyEnforceCleanForConversion, d.EnforceCleanForConversion)
	addBool(KeyConventionWarning, d.ConventionWarning)
	addString(KeyGitDomain, d.GitDomain)
	if d.WorktreeLocation != nil {
		out = append(out, configEntry{KeyWorktreeLocation, *d.WorktreeLocation})
	}
	addString(KeyAddDefaultStartPoint, d.DefaultStartPoint)
	addString(KeyHooksInstallMode, d.HooksInstallMode)

	s := &lc.ShellIntegration
	addString(KeyShellIntegrationStatus, s.Status)
	addString(KeyShellIntegrationShell, s.InstalledShell)
	addString(KeyShellIntegrationPath, s.InstalledPath)
	if s.InstalledAt != nil && !s.InstalledAt.IsZero() {
		out = append(out, configEntry{KeyShellIntegrationAt, s.InstalledAt.Format(time.RFC3339)})
	}

	b := &lc.Backup
	addBool(KeyBackupEnabled, b.Enabled)
	addBool(KeyBackupKeepBackup, b.KeepBackup)
	addInt(KeyBackupMaxBackups, b.MaxBackups)
	addInt(KeyBackupCleanupAgeDays, b.CleanupAgeDays)
	addBool(KeyBackupPreserveStashes, b.PreserveStashes)

	c := &lc.Conversion
	addBool(KeyConversionEnforceClean, c.EnforceClean)
	addBool(KeyConversionAllowDirtyForce, c.AllowDirtyForce)
	addBool(KeyConversionAutoRollback, c.AutoRollback)

	return out
}
