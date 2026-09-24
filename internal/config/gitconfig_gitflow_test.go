package config

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// isolatedRepo inits a repo under a temp dir with git's global and system
// config cut off, so only the repo's own config can answer.
func isolatedRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := filepath.Join(root, "repo")
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return dir
}

func setRepoConfig(t *testing.T, dir, key, value string) {
	t.Helper()
	if out, err := exec.Command("git", "-C", dir, "config", key, value).CombinedOutput(); err != nil {
		t.Fatalf("git config %s: %v\n%s", key, err, out)
	}
}

func TestGitflowEnabled_DefaultsOff(t *testing.T) {
	dir := isolatedRepo(t)
	if NewGitConfigIn(dir).GetBoolOrDefault(KeyGitflowEnabled) {
		t.Errorf("%s defaults to true, want false", KeyGitflowEnabled)
	}
	if d, ok := defaults[KeyGitflowEnabled]; !ok || d != "false" {
		t.Errorf("defaults[%s] = %q (registered %v), want \"false\"", KeyGitflowEnabled, d, ok)
	}
}

func TestGitflowEnabled_ReadsNamedRepo(t *testing.T) {
	dir := isolatedRepo(t)
	setRepoConfig(t, dir, KeyGitflowEnabled, "true")

	// Run from somewhere that is not the repo: the setting must come from
	// the repo NewGitConfigIn names, not from the process's cwd.
	t.Chdir(t.TempDir())
	if !NewGitConfigIn(dir).GetBoolOrDefault(KeyGitflowEnabled) {
		t.Errorf("%s=true in %s not honored", KeyGitflowEnabled, dir)
	}
}

func TestGitflowEnabled_InvalidValueFallsBackOff(t *testing.T) {
	dir := isolatedRepo(t)
	setRepoConfig(t, dir, KeyGitflowEnabled, "maybe")
	if NewGitConfigIn(dir).GetBoolOrDefault(KeyGitflowEnabled) {
		t.Errorf("unparseable %s enabled git-flow", KeyGitflowEnabled)
	}
}
