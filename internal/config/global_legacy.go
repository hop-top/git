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

// legacyScalar is one scalar key of the legacy file.
type legacyScalar struct {
	key string
	// set reports whether the file has the key; val is its value rendered
	// for git config when set.
	set bool
	val string
	// zero is what the zero-value migration wrote when the key was absent
	// ("false", "0" or ""); isString marks string keys.
	zero     string
	isString bool
}

// scalars lists every scalar key of the legacy file except installedAt,
// which both migrations wrote only when non-zero.
func (lc *legacyGlobalConfig) scalars() []legacyScalar {
	var out []legacyScalar
	addBool := func(key string, v *bool) {
		e := legacyScalar{key: key, zero: "false"}
		if v != nil {
			e.set, e.val = true, strconv.FormatBool(*v)
		}
		out = append(out, e)
	}
	addInt := func(key string, v *int) {
		e := legacyScalar{key: key, zero: "0"}
		if v != nil {
			e.set, e.val = true, strconv.Itoa(*v)
		}
		out = append(out, e)
	}
	addString := func(key string, v *string) {
		e := legacyScalar{key: key, isString: true}
		if v != nil {
			e.set, e.val = true, *v
		}
		out = append(out, e)
	}

	d := &lc.Defaults
	addBool(KeyAutoEnvStart, d.AutoEnvStart)
	addBool(KeyShowAllManagedRepos, d.ShowAllManagedRepos)
	addInt(KeyUnusedThresholdDays, d.UnusedThresholdDays)
	addBool(KeyEnforceCleanForConversion, d.EnforceCleanForConversion)
	addBool(KeyConventionWarning, d.ConventionWarning)
	addString(KeyGitDomain, d.GitDomain)
	addString(KeyWorktreeLocation, d.WorktreeLocation)
	addString(KeyAddDefaultStartPoint, d.DefaultStartPoint)
	addString(KeyHooksInstallMode, d.HooksInstallMode)

	s := &lc.ShellIntegration
	addString(KeyShellIntegrationStatus, s.Status)
	addString(KeyShellIntegrationShell, s.InstalledShell)
	addString(KeyShellIntegrationPath, s.InstalledPath)

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

// migrates reports whether the migration carries the scalar into git
// config. An empty string is skipped for every key but worktreeLocation:
// legacy git-hop wrote "gitDomain": "" by default and read "" as
// github.com, and no other string key gives "" a meaning, so writing it
// would only shadow the default. worktreeLocation "" selects the
// centralized layout and is kept.
func (e legacyScalar) migrates() bool {
	if !e.set {
		return false
	}
	return !e.isString || e.val != "" || e.key == KeyWorktreeLocation
}

// gitConfigEntries returns the hop.* entries for the keys present in the
// legacy file.
func (lc *legacyGlobalConfig) gitConfigEntries() []configEntry {
	var out []configEntry
	for _, e := range lc.scalars() {
		if e.migrates() {
			out = append(out, configEntry{e.key, e.val})
		}
	}
	s := &lc.ShellIntegration
	if s.InstalledAt != nil && !s.InstalledAt.IsZero() {
		out = append(out, configEntry{KeyShellIntegrationAt, s.InstalledAt.Format(time.RFC3339)})
	}
	return out
}
