package config

import (
	"errors"
	"fmt"
)

// staleRetired are retired settings git-hop itself wrote to --global
// for users who never chose them, so a value there says nothing about the
// user and doctor may remove it whatever it is. Earlier releases wrote
// hop.autoEnvStart=true whenever shell integration was installed or
// declined.
//
// The other retired settings are not listed: git-hop wrote them only
// through the zero-value migration, and MigrationDebris removes exactly
// those copies while keeping any value the user chose.
var staleRetired = []StaleRetiredSetting{
	{Key: keyRetiredAutoEnvStart, Replacement: KeyEnvAutoStart},
}

// StaleRetiredSetting is a --global key of a retired setting git-hop wrote
// on its own.
type StaleRetiredSetting struct {
	Key   string
	Value string
	// Replacement is the key of the setting that took its place.
	Replacement string
}

// StaleRetiredSettings lists the keys of staleRetired set in --global
// scope, with their value. git-hop never reads them.
func (l *GlobalLoader) StaleRetiredSettings() ([]StaleRetiredSetting, error) {
	var out []StaleRetiredSetting
	for _, s := range staleRetired {
		v, err := l.gc.GetGlobal(s.Key)
		if errors.Is(err, ErrKeyNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		s.Value = v
		out = append(out, s)
	}
	return out, nil
}

// RemoveStaleRetiredSetting unsets every --global value of key.
func (l *GlobalLoader) RemoveStaleRetiredSetting(key string) error {
	if _, err := l.gc.RunCmd("config", "--global", "--unset-all", key); err != nil {
		return fmt.Errorf("unset %s: %w", key, err)
	}
	return nil
}
