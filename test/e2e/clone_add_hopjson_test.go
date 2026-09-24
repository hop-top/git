package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// `git hop add` must not strip hop.json fields written by clone that the
// hub config type does not model, nor the hopspace fields that add itself
// records for the new branch in the shared local-mode file.
func TestCloneThenAdd_PreservesHopJSONFields(t *testing.T) {
	t.Parallel()
	env := SetupTestEnv(t)

	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")
	env.RunGitHop(t, env.HubPath, "add", "feat/x", "--from", "main")

	data, err := os.ReadFile(filepath.Join(env.HubPath, "hop.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Forks    map[string]any `json:"forks"`
		Repo     map[string]any `json:"repo"`
		Branches map[string]map[string]any
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("hop.json: %v\n%s", err, data)
	}

	if cfg.Forks == nil {
		t.Errorf("forks dropped:\n%s", data)
	}
	for _, k := range []string{"structure", "isBare"} {
		if _, ok := cfg.Repo[k]; !ok {
			t.Errorf("repo.%s dropped:\n%s", k, data)
		}
	}
	for _, b := range []string{"main", "feat/x"} {
		for _, k := range []string{"exists", "lastSync", "hopspaceBranch"} {
			if _, ok := cfg.Branches[b][k]; !ok {
				t.Errorf("branches.%s.%s dropped:\n%s", b, k, data)
			}
		}
	}
}
