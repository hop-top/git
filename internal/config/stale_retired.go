package config

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Keys of retired settings. git-hop reads none of them. global.json had
// most of them and the zero-value migration wrote them to git config, so
// the migration code still has to recognise them; old releases also wrote
// hop.bareRepo and hop.autoEnvStart to git config directly.
const (
	keyShowAllManagedRepos       = "hop.showAllManagedRepos"
	keyUnusedThresholdDays       = "hop.unusedThresholdDays"
	keyEnforceCleanForConversion = "hop.enforceCleanForConversion"
	keyConventionWarning         = "hop.conventionWarning"
	keyBackupEnabled             = "hop.backup.enabled"
	keyBackupPreserveStashes     = "hop.backup.preserveStashes"
	keyConversionEnforceClean    = "hop.conversion.enforceClean"
	keyConversionAllowDirtyForce = "hop.conversion.allowDirtyForce"
	keyConversionAutoRollback    = "hop.conversion.autoRollback"
	keyBareRepo                  = "hop.bareRepo"
	// keyRetiredAutoEnvStart is the key global.json's autoEnvStart
	// migrated to. Whether add and clone start the environment is
	// hop.env.autoStart, a new key, so a copy pinned by the migration or
	// by shell integration cannot turn the start on.
	keyRetiredAutoEnvStart = "hop.autoEnvStart"
)

// retiredSettings is the one list of retired settings: the migration skips
// them, MigrationDebris leaves them to doctor's retired-settings check, and
// that check removes them whatever their value. Nothing reads them, and
// git-hop wrote several on its own (the zero-value migration, shell
// integration's hop.autoEnvStart=true), so a value says nothing about the
// user's choice.
var retiredSettings = []StaleRetiredSetting{
	{Key: keyShowAllManagedRepos},
	{Key: keyUnusedThresholdDays},
	{Key: keyConventionWarning},
	{Key: keyEnforceCleanForConversion},
	{Key: keyConversionEnforceClean},
	{Key: keyConversionAllowDirtyForce},
	{Key: keyConversionAutoRollback},
	{Key: keyBackupEnabled},
	{Key: keyBackupPreserveStashes},
	{Key: keyBareRepo},
	{Key: keyRetiredAutoEnvStart, Replacement: KeyEnvAutoStart},
}

// isRetired reports whether key is one of retiredSettings.
func isRetired(key string) bool {
	for _, s := range retiredSettings {
		if s.Key == key {
			return true
		}
	}
	return false
}

// StaleRetiredSetting is a retired setting's key found in git config.
type StaleRetiredSetting struct {
	Key   string
	Value string
	// Replacement is the key of the setting that took its place, "" when
	// the setting was removed outright.
	Replacement string
}

// ConfigScope is one git config file doctor checks for retired settings:
// --global, or --local of the current hub's repository.
type ConfigScope struct {
	flag string
	gc   *GitConfig
}

// String is the scope's git config option, "--global" or "--local".
func (s ConfigScope) String() string { return s.flag }

// GlobalScope is the --global git config the loader reads.
func (l *GlobalLoader) GlobalScope() ConfigScope {
	return ConfigScope{flag: "--global", gc: l.gc}
}

// HubScope is the --local git config of the repository at hubPath. It
// reports false when hubPath is not itself a repository: git's discovery is
// stopped at hubPath (GIT_CEILING_DIRECTORIES) and GIT_DIR and friends are
// dropped, so an enclosing or unrelated repository is never taken for the
// hub's.
func HubScope(hubPath string) (ConfigScope, bool) {
	// The ceiling must name the real parent: git compares it with the
	// resolved working directory.
	dir, err := filepath.EvalSymlinks(hubPath)
	if err != nil {
		return ConfigScope{}, false
	}
	env := []string{"GIT_CEILING_DIRECTORIES=" + filepath.Dir(dir)}
	for _, kv := range os.Environ() {
		switch name, _, _ := strings.Cut(kv, "="); name {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_CEILING_DIRECTORIES":
		default:
			env = append(env, kv)
		}
	}
	gc := &GitConfig{RunCmd: func(args ...string) (string, error) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = env
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}}
	if _, err := gc.RunCmd("rev-parse", "--git-dir"); err != nil {
		return ConfigScope{}, false
	}
	return ConfigScope{flag: "--local", gc: gc}, true
}

// StaleRetiredSettings lists the retired settings set in the scope, with
// their value (the last one when the key has several, as git config --get
// reports). It reads the scope's hop.* keys in one git call.
func (s ConfigScope) StaleRetiredSettings() ([]StaleRetiredSetting, error) {
	out, err := s.gc.RunCmd("config", s.flag, "-z", "--get-regexp", `^hop\.`)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil // no hop.* key at all
		}
		return nil, fmt.Errorf("git config %s --get-regexp: %w", s.flag, err)
	}

	// -z prints each entry as name, newline, value, NUL; a key without a
	// value ("[hop] bareRepo", which git reads as true) as name, NUL.
	values := map[string]string{}
	for _, entry := range strings.Split(out, "\x00") {
		name, value, _ := strings.Cut(entry, "\n")
		values[name] = value
	}
	var found []StaleRetiredSetting
	for _, r := range retiredSettings {
		v, ok := values[canonicalKey(r.Key)]
		if !ok {
			continue
		}
		r.Value = v
		found = append(found, r)
	}
	return found, nil
}

// canonicalKey spells key the way git config --get-regexp prints it: the
// section and the variable name lowercased, a subsection as written (it is
// case-sensitive, so hop.Backup.enabled is another key).
func canonicalKey(key string) string {
	first, last := strings.Index(key, "."), strings.LastIndex(key, ".")
	return strings.ToLower(key[:first]) + key[first:last] + strings.ToLower(key[last:])
}

// RemoveStaleRetiredSetting unsets every value of key in the scope.
func (s ConfigScope) RemoveStaleRetiredSetting(key string) error {
	if _, err := s.gc.RunCmd("config", s.flag, "--unset-all", key); err != nil {
		return fmt.Errorf("unset %s: %w", key, err)
	}
	return nil
}
