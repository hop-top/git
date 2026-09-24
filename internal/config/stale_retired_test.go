package config_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"hop.top/git/internal/config"
)

// allRetiredKeys is every hop.* key git-hop dropped: the settings it never
// read, the removed bareRepo, and hop.autoEnvStart, which hop.env.autoStart
// replaced. Listed here rather than taken from the package, so a key
// dropped from the package's list fails the tests below.
var allRetiredKeys = append(append([]string{}, retiredKeys...), "hop.bareRepo", "hop.autoEnvStart")

// replacementOf is the setting that took a retired key's place, "" when
// the setting was removed outright.
func replacementOf(key string) string {
	if key == "hop.autoEnvStart" {
		return config.KeyEnvAutoStart
	}
	return ""
}

// liveKeys are settings git-hop reads, some sharing a section with a
// retired key; the retired check must never touch them.
var liveKeys = map[string]string{
	config.KeyEnvAutoStart:     "true",
	config.KeyBackupMaxBackups: "5",
	config.KeyGitDomain:        "example.com",
	"hop.backup.keepBackup":    "true",
	"hop.migrated":             "true",
}

// newHubRepo creates a bare repository standing in for a hub and returns
// its path.
func newHubRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "hub")
	if out, err := exec.Command("git", "init", "--quiet", "--bare", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return dir
}

func gitIn(t *testing.T, dir string, args ...string) (string, bool) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

func setLocal(t *testing.T, dir string, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		if out, err := exec.Command("git", "-C", dir, "config", "--local", k, v).CombinedOutput(); err != nil {
			t.Fatalf("git config --local %s %q: %v: %s", k, v, err, out)
		}
	}
}

// appendConfig adds key = value to the config file at path, as
// `git config --file path --add` would, without running git.
func appendConfig(t *testing.T, path string, kv map[string]string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for key, value := range kv {
		name := key[strings.LastIndex(key, ".")+1:]
		section := "[hop]"
		if sub := strings.TrimSuffix(strings.TrimPrefix(key, "hop."), "."+name); sub != name {
			section = fmt.Sprintf("[hop %q]", sub)
		}
		if _, err := fmt.Fprintf(f, "%s\n\t%s = %q\n", section, name, value); err != nil {
			t.Fatal(err)
		}
	}
}

func hubScope(t *testing.T, dir string) config.ConfigScope {
	t.Helper()
	s, ok := config.HubScope(dir)
	if !ok {
		t.Fatalf("HubScope(%s) = false, want the hub's --local config", dir)
	}
	return s
}

func staleRetired(t *testing.T, s config.ConfigScope) map[string]config.StaleRetiredSetting {
	t.Helper()
	entries, err := s.StaleRetiredSettings()
	if err != nil {
		t.Fatalf("StaleRetiredSettings(%s) error = %v", s, err)
	}
	got := map[string]config.StaleRetiredSetting{}
	for _, e := range entries {
		got[e.Key] = e
	}
	return got
}

// Every retired key is reported whatever its value, in --global and in the
// hub's --local config, and only in the scope that holds it. Nothing reads
// these keys, so no value is the user's choice worth keeping.
func TestStaleRetiredSettings_EveryRetiredKeyAnyValue(t *testing.T) {
	for _, key := range allRetiredKeys {
		for _, v := range []string{"true", "false", "not-a-bool"} {
			for _, where := range []string{"--global", "--local"} {
				t.Run(key+"/"+v+"/"+where, func(t *testing.T) {
					isolateGitConfig(t)
					hub := newHubRepo(t)
					global, local := os.Getenv("GIT_CONFIG_GLOBAL"), filepath.Join(hub, "config")
					appendConfig(t, global, liveKeys)
					appendConfig(t, local, liveKeys)
					if where == "--global" {
						appendConfig(t, global, map[string]string{key: v})
					} else {
						appendConfig(t, local, map[string]string{key: v})
					}

					globalScope := config.NewGlobalLoaderWithGitConfig(config.NewGitConfig()).GlobalScope()
					localScope := hubScope(t, hub)
					found, other := globalScope, localScope
					if where == "--local" {
						found, other = localScope, globalScope
					}
					if found.String() != where {
						t.Fatalf("scope = %s, want %s", found, where)
					}

					got := staleRetired(t, found)
					want := config.StaleRetiredSetting{Key: key, Value: v, Replacement: replacementOf(key)}
					if len(got) != 1 || got[key] != want {
						t.Errorf("%s stale retired = %v, want only %+v", where, got, want)
					}
					if got := staleRetired(t, other); len(got) != 0 {
						t.Errorf("%s stale retired = %v, want none", other, got)
					}
				})
			}
		}
	}
}

// Removal unsets every copy of that key in that scope, and nothing else:
// not the live keys, not the other retired keys, not the other scope's copy.
func TestRemoveStaleRetiredSetting(t *testing.T) {
	for _, where := range []string{"--global", "--local"} {
		t.Run(where, func(t *testing.T) {
			isolateGitConfig(t)
			hub := newHubRepo(t)
			setGlobal(t, liveKeys)
			setLocal(t, hub, liveKeys)
			other := map[string]string{"hop.backup.enabled": "false", "hop.autoEnvStart": "true"}
			setGlobal(t, other)
			setLocal(t, hub, other)

			scope := config.NewGlobalLoaderWithGitConfig(config.NewGitConfig()).GlobalScope()
			get := func(args ...string) (string, bool) { return gitGlobal(t, args...) }
			otherGet := func(args ...string) (string, bool) {
				return gitIn(t, hub, append([]string{"config", "--local"}, args...)...)
			}
			if where == "--local" {
				scope = hubScope(t, hub)
				get, otherGet = otherGet, get
			}
			for _, v := range []string{"true", "false"} {
				args := []string{"config", where, "--add", "hop.bareRepo", v}
				if out, err := exec.Command("git", append([]string{"-C", hub}, args...)...).CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v: %s", args, err, out)
				}
			}
			if _, ok := otherGet("--get", "hop.bareRepo"); ok {
				t.Fatalf("setup leaked hop.bareRepo into the other scope")
			}

			if err := scope.RemoveStaleRetiredSetting("hop.bareRepo"); err != nil {
				t.Fatalf("RemoveStaleRetiredSetting() error = %v", err)
			}
			if v, ok := get("--get-all", "hop.bareRepo"); ok {
				t.Errorf("hop.bareRepo still set to %q", v)
			}
			for k := range liveKeys {
				if _, ok := get("--get", k); !ok {
					t.Errorf("live %s was removed", k)
				}
			}
			for k := range other {
				if _, ok := get("--get", k); !ok {
					t.Errorf("%s was removed; only hop.bareRepo should go", k)
				}
				if _, ok := otherGet("--get", k); !ok {
					t.Errorf("%s was removed from the other scope", k)
				}
			}
			if got := staleRetired(t, scope); len(got) != len(other) {
				t.Errorf("stale retired after removal = %v, want %v", got, other)
			}
		})
	}
}

// Only the hub's own repository counts. A hub path that is no repository,
// even one nested inside another repository, has no --local config: the
// enclosing repository's config belongs to something else and --fix must
// not edit it.
func TestHubScope_OnlyTheHubsOwnRepo(t *testing.T) {
	isolateGitConfig(t)
	outer := newHubRepo(t)
	setLocal(t, outer, map[string]string{"hop.bareRepo": "true"})

	nested := filepath.Join(outer, "not-a-repo")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{nested, filepath.Join(t.TempDir(), "plain"), filepath.Join(t.TempDir(), "missing")} {
		if dir != nested && filepath.Base(dir) == "plain" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if s, ok := config.HubScope(dir); ok {
			t.Errorf("HubScope(%s) = %s, want none", dir, s)
		}
	}

	// A symlinked hub path still resolves to the hub itself.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(outer, link); err != nil {
		t.Fatal(err)
	}
	if got := staleRetired(t, hubScope(t, link)); len(got) != 1 {
		t.Errorf("stale retired via symlink = %v, want hop.bareRepo", got)
	}
}

// Keys match the way git matches them: section and variable name in any
// case, the subsection exactly. A key without a value counts (git reads
// it as true), and the last of several values is reported.
func TestStaleRetiredSettings_KeySpelling(t *testing.T) {
	isolateGitConfig(t)
	cfg := "[HOP]\n\tBAREREPO\n" +
		"[hop \"Backup\"]\n\tenabled = false\n" +
		"[hop \"conversion\"]\n\tAutoRollback = no\n" +
		"[hop]\n\tautoenvstart = true\n\tautoEnvStart = false\n"
	if err := os.WriteFile(os.Getenv("GIT_CONFIG_GLOBAL"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	scope := config.NewGlobalLoaderWithGitConfig(config.NewGitConfig()).GlobalScope()
	got := staleRetired(t, scope)
	want := map[string]string{
		"hop.bareRepo":                "",
		"hop.conversion.autoRollback": "no",
		"hop.autoEnvStart":            "false",
	}
	if len(got) != len(want) {
		t.Fatalf("stale retired = %v, want keys %v", got, want)
	}
	for k, v := range want {
		if e, ok := got[k]; !ok || e.Value != v {
			t.Errorf("%s = %+v, want value %q", k, e, v)
		}
	}

	for k := range want {
		if err := scope.RemoveStaleRetiredSetting(k); err != nil {
			t.Errorf("RemoveStaleRetiredSetting(%s) error = %v", k, err)
		}
	}
	out, ok := gitGlobal(t, "--get-regexp", `^hop\.`)
	if !ok || strings.TrimSpace(out) != "hop.Backup.enabled false" {
		t.Errorf("left after removal = %q, want only hop.Backup.enabled", out)
	}
}
