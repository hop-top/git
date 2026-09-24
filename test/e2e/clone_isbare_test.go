package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Clone always produces a bare hub (docs/stories/015-hopspace-shape-contract.md),
// whatever hop.bareRepo says. hop.json must record that layout, not the
// setting: a hub whose repo.isBare disagrees with
// `git rev-parse --is-bare-repository` misdescribes itself to every reader.
func TestClone_HopJSONRecordsActualBareLayout(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		bareRepo string // "" leaves hop.bareRepo unset
		global   bool
	}{
		{name: "setting unset, local", bareRepo: ""},
		{name: "setting false, local", bareRepo: "false"},
		{name: "setting false, global", bareRepo: "false", global: true},
		{name: "setting true, local", bareRepo: "true"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := SetupTestEnv(t)

			if tc.bareRepo != "" {
				env.RunCommand(t, env.RootDir, "git", "config", "--global", "hop.bareRepo", tc.bareRepo)
			}

			env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
			env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
			env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
			env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")

			args := []string{env.BareRepoPath, "hub"}
			if tc.global {
				args = append(args, "--global")
			}
			env.RunGitHop(t, env.RootDir, args...)

			out, err := exec.Command("git", "-C", env.HubPath,
				"rev-parse", "--is-bare-repository").CombinedOutput()
			if err != nil {
				t.Fatalf("rev-parse --is-bare-repository: %v: %s", err, out)
			}
			if got := strings.TrimSpace(string(out)); got != "true" {
				t.Fatalf("hub must be bare; rev-parse --is-bare-repository = %q", got)
			}

			data, err := os.ReadFile(filepath.Join(env.HubPath, "hop.json"))
			if err != nil {
				t.Fatal(err)
			}
			var cfg struct {
				Repo struct {
					Structure string `json:"structure"`
					IsBare    *bool  `json:"isBare"`
				} `json:"repo"`
			}
			if err := json.Unmarshal(data, &cfg); err != nil {
				t.Fatalf("hop.json: %v\n%s", err, data)
			}
			if cfg.Repo.Structure != "bare-worktree" {
				t.Errorf("repo.structure = %q, want bare-worktree\n%s", cfg.Repo.Structure, data)
			}
			if cfg.Repo.IsBare == nil || !*cfg.Repo.IsBare {
				t.Errorf("repo.isBare must be true for a bare hub\n%s", data)
			}
		})
	}
}
