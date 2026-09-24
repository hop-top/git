package config_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"hop.top/git/internal/config"
)

// isolateGitConfig points git's global config and the XDG config dir at a
// scratch directory, so nothing here reads or writes the real user config.
// It returns the git-hop config dir, where global.json.bak lives.
func isolateGitConfig(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(tmp, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := filepath.Join(tmp, ".config", "git-hop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func gitGlobal(t *testing.T, args ...string) (string, bool) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"config", "--global"}, args...)...).Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

func setGlobal(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		if out, err := exec.Command("git", "config", "--global", k, v).CombinedOutput(); err != nil {
			t.Fatalf("git config --global %s %q: %v: %s", k, v, err, out)
		}
	}
}

// buggyMigration is what the zero-value migration wrote to --global for a
// global.json.bak holding only gitDomain, worktreeLocation "" and
// backup.enabled false: the file's values for those, false/0/"" for every
// other scalar, and the sentinel.
func buggyMigration() map[string]string {
	return map[string]string{
		config.KeyAutoEnvStart:           "false",
		"hop.showAllManagedRepos":        "false",
		"hop.unusedThresholdDays":        "0",
		"hop.enforceCleanForConversion":  "false",
		"hop.conventionWarning":          "false",
		config.KeyGitDomain:              "example.com",
		config.KeyWorktreeLocation:       "",
		config.KeyAddDefaultStartPoint:   "",
		config.KeyHooksInstallMode:       "",
		config.KeyShellIntegrationStatus: "",
		config.KeyShellIntegrationShell:  "",
		config.KeyShellIntegrationPath:   "",
		"hop.backup.enabled":             "false",
		config.KeyBackupKeepBackup:       "false",
		config.KeyBackupMaxBackups:       "0",
		config.KeyBackupCleanupAgeDays:   "0",
		"hop.backup.preserveStashes":     "false",
		"hop.conversion.enforceClean":    "false",
		"hop.conversion.allowDirtyForce": "false",
		"hop.conversion.autoRollback":    "false",
		"hop.migrated":                   "true",
	}
}

const bakWithThreeKeys = `{"defaults":{"gitDomain":"example.com","worktreeLocation":""},"backup":{"enabled":false}}`

func debrisKeys(t *testing.T, l *config.GlobalLoader) []string {
	t.Helper()
	entries, err := l.MigrationDebris()
	if err != nil {
		t.Fatalf("MigrationDebris() error = %v", err)
	}
	var keys []string
	for _, e := range entries {
		keys = append(keys, e.Key)
	}
	sort.Strings(keys)
	return keys
}

// Debris is a --global key the buggy migration invented: absent from the
// .bak (or "" there, except worktreeLocation) and still holding the zero
// value it wrote. Keys the .bak carries, and keys the user has since
// changed, are the user's and stay.
func TestMigrationDebris(t *testing.T) {
	dir := isolateGitConfig(t)
	if err := os.WriteFile(filepath.Join(dir, "global.json.bak"), []byte(bakWithThreeKeys), 0o644); err != nil {
		t.Fatal(err)
	}
	setGlobal(t, buggyMigration())
	// Changed since the migration: no longer the zero value it wrote.
	setGlobal(t, map[string]string{config.KeyAutoEnvStart: "true", config.KeyBackupMaxBackups: "5"})

	l := config.NewGlobalLoaderWithGitConfig(config.NewGitConfig())
	// The quoted keys are retired settings git-hop no longer has; the
	// migration wrote them all the same, so they are debris too.
	want := []string{
		config.KeyAddDefaultStartPoint,
		config.KeyBackupCleanupAgeDays,
		config.KeyBackupKeepBackup,
		"hop.backup.preserveStashes",
		"hop.conventionWarning",
		"hop.conversion.allowDirtyForce",
		"hop.conversion.autoRollback",
		"hop.conversion.enforceClean",
		"hop.enforceCleanForConversion",
		config.KeyHooksInstallMode,
		config.KeyShellIntegrationPath,
		config.KeyShellIntegrationShell,
		config.KeyShellIntegrationStatus,
		"hop.showAllManagedRepos",
		"hop.unusedThresholdDays",
	}
	sort.Strings(want)
	got := debrisKeys(t, l)
	if len(got) != len(want) {
		t.Fatalf("debris = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("debris = %v, want %v", got, want)
		}
	}

	entries, _ := l.MigrationDebris()
	if err := l.RemoveMigrationDebris(entries); err != nil {
		t.Fatalf("RemoveMigrationDebris() error = %v", err)
	}
	for _, k := range want {
		if v, ok := gitGlobal(t, "--get", k); ok {
			t.Errorf("%s still set to %q after repair", k, v)
		}
	}
	for _, k := range []string{
		config.KeyAutoEnvStart, config.KeyGitDomain, config.KeyWorktreeLocation,
		"hop.backup.enabled", config.KeyBackupMaxBackups, "hop.migrated",
	} {
		if _, ok := gitGlobal(t, "--get", k); !ok {
			t.Errorf("%s was removed; it is the user's", k)
		}
	}
	if got := debrisKeys(t, l); len(got) != 0 {
		t.Errorf("debris after repair = %v, want none", got)
	}
}

// An empty string in the .bak was never migrated by the fixed migration
// (except worktreeLocation), so the "" the buggy one wrote is debris too.
func TestMigrationDebris_EmptyStringInBak(t *testing.T) {
	dir := isolateGitConfig(t)
	bak := `{"defaults":{"gitDomain":"","worktreeLocation":""}}`
	if err := os.WriteFile(filepath.Join(dir, "global.json.bak"), []byte(bak), 0o644); err != nil {
		t.Fatal(err)
	}
	setGlobal(t, map[string]string{
		config.KeyGitDomain:        "",
		config.KeyWorktreeLocation: "",
		"hop.migrated":             "true",
	})

	got := debrisKeys(t, config.NewGlobalLoaderWithGitConfig(config.NewGitConfig()))
	if len(got) != 1 || got[0] != config.KeyGitDomain {
		t.Errorf("debris = %v, want [%s]", got, config.KeyGitDomain)
	}
}

// The repair only ever acts on a migrated install that still has the
// .bak to compare against; without either there is no evidence.
func TestMigrationDebris_Gates(t *testing.T) {
	t.Run("no global.json.bak", func(t *testing.T) {
		isolateGitConfig(t)
		setGlobal(t, buggyMigration())
		if got := debrisKeys(t, config.NewGlobalLoaderWithGitConfig(config.NewGitConfig())); len(got) != 0 {
			t.Errorf("debris = %v, want none", got)
		}
	})
	t.Run("hop.migrated unset", func(t *testing.T) {
		dir := isolateGitConfig(t)
		if err := os.WriteFile(filepath.Join(dir, "global.json.bak"), []byte(bakWithThreeKeys), 0o644); err != nil {
			t.Fatal(err)
		}
		kv := buggyMigration()
		delete(kv, "hop.migrated")
		setGlobal(t, kv)
		if got := debrisKeys(t, config.NewGlobalLoaderWithGitConfig(config.NewGitConfig())); len(got) != 0 {
			t.Errorf("debris = %v, want none", got)
		}
	})
}
