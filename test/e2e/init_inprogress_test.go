package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInit_RefusesOperationInProgress: `git hop init` refuses a bare
// conversion while a merge is stopped before its commit, with the working
// tree clean. It fails like git, with a fatal naming the operation and a
// hint to conclude it, before changing anything; --force does not
// override it, and --dry-run reports the same refusal.
func TestInit_RefusesOperationInProgress(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	for _, tc := range []struct {
		name  string
		args  []string
		retry string // the command line the hint offers after concluding
	}{
		{"plain", []string{"init", "--no-prompt"}, "git hop init --no-prompt"},
		{"force", []string{"init", "--no-prompt", "--force"}, "git hop init --no-prompt --force"},
		{"dry-run", []string{"init", "--no-prompt", "--dry-run"}, "git hop init --no-prompt --dry-run"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := SetupTestEnv(t)
			repo := seedPlainRepo(t, env, "proj", "main")
			env.RunCommand(t, repo, "git", "checkout", "-q", "-b", "other")
			env.RunCommand(t, repo, "git", "commit", "-q", "--allow-empty", "-m", "other")
			env.RunCommand(t, repo, "git", "checkout", "-q", "main")
			env.RunCommand(t, repo, "git", "merge", "--no-commit", "-s", "ours", "other")
			if _, err := os.Stat(filepath.Join(repo, ".git", "MERGE_HEAD")); err != nil {
				t.Fatalf("setup left no merge in progress: %v", err)
			}

			stdout, stderr, code := env.RunCommandWithExit(t, repo, env.BinPath, tc.args...)
			if code != 1 {
				t.Fatalf("exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
			}
			for _, want := range []string{
				"fatal: a merge is in progress",
				"hint: finish the merge with 'git merge --continue' or abort it with 'git merge --abort'",
				"hint: then run " + tc.retry + " again\n",
			} {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr lacks %q:\n%s", want, stderr)
				}
			}
			if strings.Contains(stdout, "Conversion plan") {
				t.Errorf("a refused dry run still printed a plan:\n%s", stdout)
			}
			if _, err := os.Stat(filepath.Join(repo, ".git", "MERGE_HEAD")); err != nil {
				t.Errorf("merge state gone after the refusal: %v", err)
			}
			if _, err := os.Stat(filepath.Join(repo, "hop.json")); !os.IsNotExist(err) {
				t.Errorf("hop.json written despite the refusal")
			}
		})
	}
}
