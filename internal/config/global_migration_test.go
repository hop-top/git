package config_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"hop.top/git/internal/config"
)

// writeLegacyGlobalJSON isolates the XDG config dir under t.TempDir() and
// writes raw as the legacy global.json.
func writeLegacyGlobalJSON(t *testing.T, raw string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	dir := filepath.Join(tmp, ".config", "git-hop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "global.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hopKeys(store map[string]string) []string {
	var keys []string
	for k := range store {
		if strings.HasPrefix(k, "hop.") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// A legacy file only carries the keys the user wrote. Keys it lacks must not
// be written to git config: an existing key, even false/0/"", shadows the
// compiled default.
func TestMigration_WritesOnlyKeysPresentInLegacyFile(t *testing.T) {
	writeLegacyGlobalJSON(t, `{"defaults":{"gitDomain":"example.com"}}`)

	store := map[string]string{}
	loader := config.NewGlobalLoaderWithGitConfig(fakeGitConfig(store))
	cfg := loader.Load()

	want := []string{"hop.gitDomain", "hop.migrated"}
	if got := hopKeys(store); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("git config keys after migration = %v, want %v", got, want)
	}
	if store["hop.gitDomain"] != "example.com" {
		t.Errorf("hop.gitDomain = %q, want example.com", store["hop.gitDomain"])
	}

	wantCfg := loader.GetDefaults()
	wantCfg.Defaults.GitDomain = "example.com"
	if cfg.Defaults != wantCfg.Defaults {
		t.Errorf("Defaults after migration = %+v, want %+v", cfg.Defaults, wantCfg.Defaults)
	}
	if cfg.Backup != wantCfg.Backup {
		t.Errorf("Backup after migration = %+v, want %+v", cfg.Backup, wantCfg.Backup)
	}
	if cfg.ShellIntegration.Status != "unknown" {
		t.Errorf("ShellIntegration.Status = %q, want unknown", cfg.ShellIntegration.Status)
	}
}

// Explicit values in the legacy file, including false and 0, are the user's
// choice and must survive migration.
func TestMigration_KeepsExplicitZeroValues(t *testing.T) {
	writeLegacyGlobalJSON(t, `{
		"backup": {"keepBackup": false, "maxBackups": 0}
	}`)

	store := map[string]string{}
	loader := config.NewGlobalLoaderWithGitConfig(fakeGitConfig(store))
	cfg := loader.Load()

	for key, want := range map[string]string{
		"hop.backup.keepBackup": "false",
		"hop.backup.maxBackups": "0",
		"hop.migrated":          "true",
	} {
		if got, ok := store[key]; !ok || got != want {
			t.Errorf("store[%s] = %q (present=%v), want %q", key, got, ok, want)
		}
	}
	if n := len(hopKeys(store)); n != 3 {
		t.Errorf("git config holds %d hop.* keys, want 3: %v", n, hopKeys(store))
	}
	if cfg.Backup.MaxBackups != 0 || cfg.Backup.KeepBackup {
		t.Errorf("explicit zero values lost: %+v %+v", cfg.Backup, cfg.Defaults)
	}
}

// Legacy git-hop wrote "gitDomain": "" (its defaults had no domain) and read
// "" as github.com. No string key but worktreeLocation gives "" a meaning,
// so an empty string is not migrated; worktreeLocation "" selects the
// centralized layout and is kept.
func TestMigration_EmptyStrings(t *testing.T) {
	writeLegacyGlobalJSON(t, `{
		"defaults": {"gitDomain": "", "worktreeLocation": "", "defaultStartPoint": "", "hooksInstallMode": ""},
		"shellIntegration": {"status": "", "installedShell": "", "installedPath": ""}
	}`)

	store := map[string]string{}
	loader := config.NewGlobalLoaderWithGitConfig(fakeGitConfig(store))
	cfg := loader.Load()

	want := []string{"hop.migrated", "hop.worktreeLocation"}
	if got := hopKeys(store); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("git config keys after migration = %v, want %v", got, want)
	}
	if v, ok := store["hop.worktreeLocation"]; !ok || v != "" {
		t.Errorf("hop.worktreeLocation = %q (present=%v), want empty and present", v, ok)
	}
	if cfg.Defaults.GitDomain != "github.com" {
		t.Errorf("GitDomain = %q, want github.com", cfg.Defaults.GitDomain)
	}
	if cfg.Defaults.DefaultStartPoint != "default-branch" {
		t.Errorf("DefaultStartPoint = %q, want default-branch", cfg.Defaults.DefaultStartPoint)
	}
	if cfg.ShellIntegration.Status != "unknown" {
		t.Errorf("ShellIntegration.Status = %q, want unknown", cfg.ShellIntegration.Status)
	}
}
