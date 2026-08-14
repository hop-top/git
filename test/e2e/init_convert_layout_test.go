package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// seedPlainRepo materializes a plain (non-hop) git repo with one commit on
// the named default branch — the exact starting shape `git hop init`
// converts.
func seedPlainRepo(t *testing.T, env *TestEnv, name, defaultBranch string) string {
	t.Helper()

	repoPath := filepath.Join(env.RootDir, name)
	env.RunCommand(t, env.RootDir, "git", "init", "-b", defaultBranch, repoPath)
	WriteFile(t, filepath.Join(repoPath, "README.md"), "# "+name+"\n")
	env.RunCommand(t, repoPath, "git", "add", "README.md")
	env.RunCommand(t, repoPath, "git", "commit", "-m", "Initial commit")

	return repoPath
}

// assertConvertedLayout is the invariant `git hop init` must uphold: the
// worktree directory on disk, the path recorded in hop.json, and the target
// of the `current` symlink all name the SAME real directory — hops/<branch>.
func assertConvertedLayout(t *testing.T, repoPath, branch string) {
	t.Helper()

	// 1. hops/<branch>/ exists and holds the checkout.
	wantWorktree := filepath.Join(repoPath, "hops", branch)
	info, err := os.Stat(wantWorktree)
	if err != nil {
		entries, _ := os.ReadDir(filepath.Join(repoPath, "hops"))
		var got []string
		for _, e := range entries {
			got = append(got, e.Name())
		}
		t.Fatalf("worktree dir %s missing after init; hops/ contains %v", wantWorktree, got)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", wantWorktree)
	}
	if _, err := os.Stat(filepath.Join(wantWorktree, "README.md")); err != nil {
		t.Errorf("checkout missing from %s: %v", wantWorktree, err)
	}

	// 2. hops/ holds nothing else — no SHA-named stray worktree.
	entries, err := os.ReadDir(filepath.Join(repoPath, "hops"))
	if err != nil {
		t.Fatalf("read hops/: %v", err)
	}
	for _, e := range entries {
		if e.Name() != branch {
			t.Errorf("unexpected entry in hops/: %q (want only %q)", e.Name(), branch)
		}
	}

	// 3. hop.json records a path that actually resolves.
	raw, err := os.ReadFile(filepath.Join(repoPath, "hop.json"))
	if err != nil {
		t.Fatalf("read hop.json: %v", err)
	}
	var cfg struct {
		Repo struct {
			DefaultBranch string `json:"defaultBranch"`
		} `json:"repo"`
		Branches map[string]struct {
			Path string `json:"path"`
		} `json:"branches"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parse hop.json: %v", err)
	}
	if cfg.Repo.DefaultBranch != branch {
		t.Errorf("hop.json defaultBranch = %q, want %q", cfg.Repo.DefaultBranch, branch)
	}
	entry, ok := cfg.Branches[branch]
	if !ok {
		t.Fatalf("hop.json has no branches.%s entry (branches: %v)", branch, cfg.Branches)
	}
	if want := filepath.Join("hops", branch); entry.Path != want {
		t.Errorf("hop.json branches.%s.path = %q, want %q", branch, entry.Path, want)
	}
	recorded := filepath.Join(repoPath, entry.Path)
	if _, err := os.Stat(recorded); err != nil {
		t.Errorf("hop.json records path %q which does not exist: %v", entry.Path, err)
	}

	// 4. The `current` symlink resolves to that same real directory.
	currentLink := filepath.Join(repoPath, "current")
	target, err := os.Readlink(currentLink)
	if err != nil {
		t.Fatalf("current is not a symlink: %v", err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(repoPath, target)
	}
	resolved, err := os.Stat(target)
	if err != nil {
		t.Fatalf("current symlink dangles (-> %s): %v", target, err)
	}
	if !resolved.IsDir() {
		t.Errorf("current resolves to a non-directory: %s", target)
	}
	gotAbs, _ := filepath.EvalSymlinks(currentLink)
	wantAbs, _ := filepath.EvalSymlinks(wantWorktree)
	if gotAbs != wantAbs {
		t.Errorf("current resolves to %s, want %s", gotAbs, wantAbs)
	}
}

// TestInit_ConvertNamesWorktreeAfterBranch is the regression guard for
// `git hop init` naming the converted worktree directory after the resolved
// commit SHA instead of the branch. The success banner told users to `cd`
// into hops/main while the checkout actually lived at hops/<40-char-sha>,
// leaving `current` dangling and hop.json pointing at a nonexistent path.
func TestInit_ConvertNamesWorktreeAfterBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)
	repoPath := seedPlainRepo(t, env, "convert-main", "main")

	out := env.RunGitHopCombined(t, repoPath, "init", "--hooks", "none", "--no-prompt")
	t.Logf("init output:\n%s", out)

	assertConvertedLayout(t, repoPath, "main")
}

// TestInit_ConvertNonDefaultBranchName covers a repo whose default branch
// is not "main". The conversion path hardcodes "main" in places, so this
// pins that the recorded branch drives the directory name.
func TestInit_ConvertNonDefaultBranchName(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)
	repoPath := seedPlainRepo(t, env, "convert-trunk", "trunk")

	out := env.RunGitHopCombined(t, repoPath, "init", "--hooks", "none", "--no-prompt")
	t.Logf("init output:\n%s", out)

	assertConvertedLayout(t, repoPath, "trunk")
}

// TestInit_ConvertThenAdd verifies `git hop add` still lands a new branch at
// hops/<branch> alongside the converted default worktree.
func TestInit_ConvertThenAdd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	env := SetupTestEnv(t)
	repoPath := seedPlainRepo(t, env, "convert-add", "main")

	env.RunGitHopCombined(t, repoPath, "init", "--hooks", "none", "--no-prompt")
	out := env.RunGitHopCombined(t, repoPath, "add", "feature")
	t.Logf("add output:\n%s", out)

	added := filepath.Join(repoPath, "hops", "feature")
	if _, err := os.Stat(added); err != nil {
		entries, _ := os.ReadDir(filepath.Join(repoPath, "hops"))
		var got []string
		for _, e := range entries {
			got = append(got, e.Name())
		}
		t.Fatalf("git hop add did not create %s; hops/ contains %v\nadd output:\n%s", added, got, out)
	}
	if _, err := os.Stat(filepath.Join(repoPath, "hops", "main")); err != nil {
		t.Errorf("default worktree hops/main lost after add: %v", err)
	}
}
