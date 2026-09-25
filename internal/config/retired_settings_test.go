package config_test

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
)

// retiredKeys are hop.* settings git-hop modeled but never read. They are
// gone from the model, but old files and git config still carry them.
var retiredKeys = []string{
	"hop.showAllManagedRepos",
	"hop.unusedThresholdDays",
	"hop.conventionWarning",
	"hop.enforceCleanForConversion",
	"hop.conversion.enforceClean",
	"hop.conversion.allowDirtyForce",
	"hop.conversion.autoRollback",
	"hop.backup.enabled",
	"hop.backup.preserveStashes",
}

// legacyWithRetired is a global.json carrying every retired setting (with
// non-default values) next to three live ones.
const legacyWithRetired = `{
	"defaults": {"autoEnvStart": false, "gitDomain": "example.com",
		"showAllManagedRepos": true, "unusedThresholdDays": 9,
		"enforceCleanForConversion": false, "conventionWarning": false},
	"backup": {"enabled": false, "preserveStashes": false, "maxBackups": 4},
	"conversion": {"enforceClean": false, "allowDirtyForce": true, "autoRollback": false}
}`

// The migration carries live settings only; retired ones would be dead
// keys in the user's global git config.
func TestMigration_DropsRetiredSettings(t *testing.T) {
	writeLegacyGlobalJSON(t, legacyWithRetired)

	store := map[string]string{}
	loader := config.NewGlobalLoaderWithGitConfig(fakeGitConfig(store))
	cfg := loader.Load()

	want := []string{"hop.backup.maxBackups", "hop.gitDomain", "hop.migrated"}
	if got := hopKeys(store); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("git config keys after migration = %v, want %v", got, want)
	}
	if cfg.Defaults.GitDomain != "example.com" || cfg.Defaults.EnvAutoStart || cfg.Backup.MaxBackups != 4 {
		t.Errorf("live settings not migrated: %+v %+v", cfg.Defaults, cfg.Backup)
	}
}

// Retired keys left in git config load without error and change nothing.
func TestLoad_IgnoresRetiredSettingsInGitConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store := map[string]string{}
	for _, k := range retiredKeys {
		store[k] = "true"
	}
	store["hop.unusedThresholdDays"] = "9"
	store["hop.showAllManagedRepos"] = "not-a-bool"

	loader := config.NewGlobalLoaderWithGitConfig(fakeGitConfig(store))
	cfg := loader.Load()
	if defs := loader.GetDefaults(); !reflect.DeepEqual(cfg, defs) {
		t.Errorf("Load with retired keys = %+v\nwant defaults %+v", cfg, defs)
	}
}

// Files written by older git-hop versions carry the retired settings (and
// volumes.json its cleanup block). They must still load, and a rewrite must
// keep what the model no longer knows.
func TestOldFilesWithRetiredSettingsStillLoad(t *testing.T) {
	t.Run("volumes.json", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		writeFile(t, fs, "/hs/volumes.json", `{
			"basePath": "/vols",
			"branches": {"main": {"volumes": {"db": "main_db"}}},
			"cleanup": {"orphaned": "manual", "unusedThresholdDays": 30}
		}`)
		cfg, err := config.NewLoader(fs).LoadVolumesConfig("/hs")
		if err != nil {
			t.Fatalf("LoadVolumesConfig() error = %v", err)
		}
		if cfg.BasePath != "/vols" || cfg.Branches["main"].Volumes["db"] != "main_db" {
			t.Errorf("loaded %+v", cfg)
		}

		cfg.Branches["feat"] = config.BranchVolumes{Volumes: map[string]string{"db": "feat_db"}}
		if err := config.NewWriter(fs).WriteVolumesConfig("/hs", cfg); err != nil {
			t.Fatalf("WriteVolumesConfig() error = %v", err)
		}
		doc := readJSON(t, fs, "/hs/volumes.json")
		cleanup, _ := doc["cleanup"].(map[string]any)
		if cleanup["orphaned"] != "manual" || cleanup["unusedThresholdDays"] != float64(30) {
			t.Errorf("rewrite lost cleanup: %v", doc["cleanup"])
		}
	})

	const hopJSON = `{
		"repo": {"uri": "file:///origin", "org": "acme", "repo": "widget", "defaultBranch": "main"},
		"branches": {"main": {"path": "hops/main", "hopspaceBranch": "main", "followsConvention": true}},
		"settings": {"envPatterns": [], "conventionWarning": false, "showAllManagedRepos": true},
		"conversion": {"autoRollback": false}
	}`

	t.Run("hop.json hub", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		writeFile(t, fs, "/hub/hop.json", hopJSON)
		cfg, err := config.NewLoader(fs).LoadHubConfig("/hub")
		if err != nil {
			t.Fatalf("LoadHubConfig() error = %v", err)
		}
		if cfg.Repo.Repo != "widget" || cfg.Branches["main"].Path != "hops/main" {
			t.Errorf("loaded %+v", cfg)
		}
		if err := config.NewWriter(fs).WriteHubConfig("/hub", cfg); err != nil {
			t.Fatalf("WriteHubConfig() error = %v", err)
		}
		doc := readJSON(t, fs, "/hub/hop.json")
		settings, _ := doc["settings"].(map[string]any)
		if settings["showAllManagedRepos"] != true {
			t.Errorf("rewrite lost settings.showAllManagedRepos: %v", doc["settings"])
		}
		if _, ok := doc["conversion"]; !ok {
			t.Errorf("rewrite lost conversion: %v", doc)
		}
	})

	t.Run("hop.json hopspace", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		writeFile(t, fs, "/hs/hop.json", hopJSON)
		cfg, err := config.NewLoader(fs).LoadHopspaceConfig("/hs")
		if err != nil {
			t.Fatalf("LoadHopspaceConfig() error = %v", err)
		}
		if cfg.Repo.Org != "acme" {
			t.Errorf("loaded %+v", cfg)
		}
	})

	t.Run("config.json", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		t.Setenv("XDG_CONFIG_HOME", "/xdg")
		writeFile(t, fs, filepath.Join(config.GetConfigHome(), "config.json"), `{
			"defaults": {"gitDomain": "example.com", "showAllManagedRepos": true,
				"unusedThresholdDays": 9, "conventionWarning": false,
				"enforceCleanForConversion": false},
			"backup": {"enabled": false, "preserveStashes": false},
			"conversion": {"enforceClean": false, "allowDirtyForce": true, "autoRollback": false}
		}`)
		cfg, err := config.LoadSchemaConfig(fs)
		if err != nil {
			t.Fatalf("LoadSchemaConfig() error = %v", err)
		}
		if cfg.Defaults.GitDomain != "example.com" {
			t.Errorf("GitDomain = %q, want example.com", cfg.Defaults.GitDomain)
		}
	})
}

func writeFile(t *testing.T, fs afero.Fs, path, content string) {
	t.Helper()
	if err := afero.WriteFile(fs, path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, fs afero.Fs, path string) map[string]any {
	t.Helper()
	data, err := afero.ReadFile(fs, path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return doc
}
