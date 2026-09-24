package cmd

import (
	"errors"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
)

// conversionBackupRoot resolves hop.backup.path through gc: git expands
// `~/` (--type=path), unset or empty selects the default cache root, and
// a relative value is an error naming the key.
func conversionBackupRoot(gc *config.GitConfig) (string, error) {
	configured, err := gc.GetPath(config.KeyBackupPath)
	if err != nil && !errors.Is(err, config.ErrKeyNotFound) {
		return "", err
	}
	return hop.ResolveConversionBackupRoot(configured)
}

// conversionBackupRetention reads hop.backup.maxBackups and
// hop.backup.cleanupAgeDays through gc. A missing or unparseable value
// takes the compiled default; zero or less turns that limit off.
func conversionBackupRetention(gc *config.GitConfig) hop.ConversionBackupRetention {
	return hop.ConversionBackupRetention{
		MaxBackups: gc.GetIntOrDefault(config.KeyBackupMaxBackups),
		MaxAgeDays: gc.GetIntOrDefault(config.KeyBackupCleanupAgeDays),
	}
}
