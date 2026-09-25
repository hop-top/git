package config_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hop.top/git/internal/config"
)

// writeManagersJSON writes content as managers.json under a throwaway
// XDG_CONFIG_HOME and returns its path.
func writeManagersJSON(t *testing.T, content string) string {
	t.Helper()
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	dir := filepath.Join(xdg, "git-hop")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "managers.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// captureStderr returns what fn wrote to os.Stderr.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()
	fn()
	w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

// A broken managers.json is unrelated to the hop.* git-config settings;
// those must still apply, and the broken file must be named in a warning
// rather than dropped silently.
func TestLoad_BrokenManagersJSONKeepsGitConfigSettings(t *testing.T) {
	path := writeManagersJSON(t, "{ not json")
	store := map[string]string{
		"hop.worktreeLocation":  "/custom/{branch}",
		"hop.gitDomain":         "gitlab.com",
		"hop.backup.maxBackups": "9",
	}
	loader := config.NewGlobalLoaderWithGitConfig(fakeGitConfig(store))

	var cfg *config.GlobalConfig
	stderr := captureStderr(t, func() { cfg = loader.Load() })

	if cfg.Defaults.WorktreeLocation != "/custom/{branch}" {
		t.Errorf("WorktreeLocation = %q, want /custom/{branch}", cfg.Defaults.WorktreeLocation)
	}
	if cfg.Defaults.GitDomain != "gitlab.com" {
		t.Errorf("GitDomain = %q, want gitlab.com", cfg.Defaults.GitDomain)
	}
	if cfg.Backup.MaxBackups != 9 {
		t.Errorf("Backup.MaxBackups = %d, want 9", cfg.Backup.MaxBackups)
	}
	if len(cfg.PackageManagers) != 0 || len(cfg.EnvironmentManagers) != 0 {
		t.Errorf("managers from a broken file = %+v %+v, want none", cfg.PackageManagers, cfg.EnvironmentManagers)
	}
	if !strings.Contains(stderr, "warning:") || !strings.Contains(stderr, path) {
		t.Errorf("stderr = %q, want a warning naming %s", stderr, path)
	}
}

func TestManagersFileError(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		if err := config.NewGlobalLoader().ManagersFileError(); err != nil {
			t.Errorf("ManagersFileError() = %v, want nil", err)
		}
	})
	t.Run("valid", func(t *testing.T) {
		writeManagersJSON(t, `{"packageManagers": [{"name": "custom-pm"}]}`)
		if err := config.NewGlobalLoader().ManagersFileError(); err != nil {
			t.Errorf("ManagersFileError() = %v, want nil", err)
		}
	})
	t.Run("broken", func(t *testing.T) {
		path := writeManagersJSON(t, "{ not json")
		err := config.NewGlobalLoader().ManagersFileError()
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("ManagersFileError() = %v, want an error naming %s", err, path)
		}
	})
}

// A valid managers.json still loads next to the git-config settings.
func TestLoad_ValidManagersJSON(t *testing.T) {
	writeManagersJSON(t, `{"packageManagers": [{"name": "custom-pm"}]}`)
	store := map[string]string{"hop.gitDomain": "gitlab.com"}
	cfg := config.NewGlobalLoaderWithGitConfig(fakeGitConfig(store)).Load()
	if len(cfg.PackageManagers) != 1 || cfg.PackageManagers[0].Name != "custom-pm" {
		t.Errorf("PackageManagers = %+v, want custom-pm", cfg.PackageManagers)
	}
	if cfg.Defaults.GitDomain != "gitlab.com" {
		t.Errorf("GitDomain = %q, want gitlab.com", cfg.Defaults.GitDomain)
	}
}
