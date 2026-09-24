package config_test

import (
	"os/exec"
	"testing"

	"hop.top/git/internal/config"
)

func staleRetiredKeys(t *testing.T, l *config.GlobalLoader) map[string]string {
	t.Helper()
	entries, err := l.StaleRetiredSettings()
	if err != nil {
		t.Fatalf("StaleRetiredSettings() error = %v", err)
	}
	got := map[string]string{}
	for _, e := range entries {
		if e.Replacement != config.KeyEnvAutoStart {
			t.Errorf("%s replacement = %q, want %s", e.Key, e.Replacement, config.KeyEnvAutoStart)
		}
		got[e.Key] = e.Value
	}
	return got
}

// hop.autoEnvStart is reported whatever its value: git-hop wrote it for
// users who never chose it, and nothing reads it.
func TestStaleRetiredSettings_AutoEnvStartAnyValue(t *testing.T) {
	for _, v := range []string{"true", "false", "not-a-bool"} {
		t.Run(v, func(t *testing.T) {
			isolateGitConfig(t)
			setGlobal(t, map[string]string{"hop.autoEnvStart": v})

			got := staleRetiredKeys(t, config.NewGlobalLoaderWithGitConfig(config.NewGitConfig()))
			if len(got) != 1 || got["hop.autoEnvStart"] != v {
				t.Errorf("stale retired = %v, want only hop.autoEnvStart=%q", got, v)
			}
		})
	}
}

// Nothing to report without the key. The live hop.env.autoStart and the
// other retired settings (which the migration-debris check owns, keeping
// the values the user chose) are not this check's.
func TestStaleRetiredSettings_IgnoresOtherKeys(t *testing.T) {
	isolateGitConfig(t)
	setGlobal(t, map[string]string{
		config.KeyEnvAutoStart: "true",
		"hop.backup.enabled":   "false",
		"hop.bareRepo":         "true",
		config.KeyGitDomain:    "example.com",
	})

	if got := staleRetiredKeys(t, config.NewGlobalLoaderWithGitConfig(config.NewGitConfig())); len(got) != 0 {
		t.Errorf("stale retired = %v, want none", got)
	}
}

// Removal unsets that key only, every copy of it, and nothing else.
func TestRemoveStaleRetiredSetting(t *testing.T) {
	isolateGitConfig(t)
	setGlobal(t, map[string]string{
		config.KeyEnvAutoStart: "true",
		"hop.backup.enabled":   "false",
	})
	for _, v := range []string{"true", "false"} {
		if out, err := exec.Command("git", "config", "--global", "--add", "hop.autoEnvStart", v).CombinedOutput(); err != nil {
			t.Fatalf("git config --add: %v: %s", err, out)
		}
	}

	l := config.NewGlobalLoaderWithGitConfig(config.NewGitConfig())
	if err := l.RemoveStaleRetiredSetting("hop.autoEnvStart"); err != nil {
		t.Fatalf("RemoveStaleRetiredSetting() error = %v", err)
	}
	if v, ok := gitGlobal(t, "--get-all", "hop.autoEnvStart"); ok {
		t.Errorf("hop.autoEnvStart still set to %q", v)
	}
	for _, k := range []string{config.KeyEnvAutoStart, "hop.backup.enabled"} {
		if _, ok := gitGlobal(t, "--get", k); !ok {
			t.Errorf("%s was removed; only hop.autoEnvStart should go", k)
		}
	}
	if got := staleRetiredKeys(t, l); len(got) != 0 {
		t.Errorf("stale retired after removal = %v, want none", got)
	}
}
