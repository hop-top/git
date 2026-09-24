package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// DebrisEntry is a --global hop.* key left by the zero-value migration.
type DebrisEntry struct {
	Key   string
	Value string
}

// MigrationDebris lists the --global hop.* keys the zero-value migration
// wrote that the user never set.
//
// That migration decoded global.json into GlobalConfig and wrote every
// scalar to git config --global, so keys absent from the file landed as
// false, 0 or "" and shadow the compiled defaults. A key is debris when
// global.json.bak (the file the migration read) lacks it, or has it as ""
// for a string key other than worktreeLocation, and git config still holds
// exactly the zero value the migration wrote. Anything else is the user's.
//
// It returns nothing unless the install was migrated (hop.migrated=true)
// and global.json.bak still exists: without the file there is no record of
// what the user wrote.
func (l *GlobalLoader) MigrationDebris() ([]DebrisEntry, error) {
	content, err := os.ReadFile(getGlobalConfigPath() + ".bak")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read legacy backup: %w", err)
	}
	if v, err := l.gc.GetGlobal("hop.migrated"); err != nil || v != "true" {
		return nil, nil
	}

	var legacy legacyGlobalConfig
	if err := json.Unmarshal(content, &legacy); err != nil {
		return nil, fmt.Errorf("parse legacy backup: %w", err)
	}

	var out []DebrisEntry
	for _, e := range legacy.scalars() {
		// Retired settings included: the zero-value migration wrote them too.
		if e.userSet() {
			continue
		}
		written := e.zero
		if e.set {
			written = e.val // "" for a string key the file had empty
		}
		v, err := l.gc.GetGlobal(e.key)
		if errors.Is(err, ErrKeyNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if v == written {
			out = append(out, DebrisEntry{Key: e.key, Value: v})
		}
	}
	return out, nil
}

// RemoveMigrationDebris unsets the given keys from --global scope.
func (l *GlobalLoader) RemoveMigrationDebris(entries []DebrisEntry) error {
	for _, e := range entries {
		if err := l.gc.UnsetGlobal(e.Key); err != nil {
			return fmt.Errorf("unset %s: %w", e.Key, err)
		}
	}
	return nil
}
