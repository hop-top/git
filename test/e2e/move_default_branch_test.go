package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupDefaultMoveEnv is setupMoveEnv with `current` back on main and a
// recorded base on feature/old, so a rename of main has something to carry.
func setupDefaultMoveEnv(t *testing.T) *TestEnv {
	t.Helper()
	env, _ := setupMoveEnv(t)
	pointCurrentAt(t, env, "main")
	return env
}

func pointCurrentAt(t *testing.T, env *TestEnv, branch string) {
	t.Helper()
	link := filepath.Join(env.HubPath, "current")
	_ = os.Remove(link)
	if err := os.Symlink(filepath.Join("hops", branch), link); err != nil {
		t.Fatal(err)
	}
}

func readHubJSON(t *testing.T, env *TestEnv) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(env.HubPath, "hop.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func hubDefaultBranch(t *testing.T, env *TestEnv) string {
	t.Helper()
	repo, _ := readHubJSON(t, env)["repo"].(map[string]interface{})
	s, _ := repo["defaultBranch"].(string)
	return s
}

func hubBranchEntry(t *testing.T, env *TestEnv, name string) (map[string]interface{}, bool) {
	t.Helper()
	branches, _ := readHubJSON(t, env)["branches"].(map[string]interface{})
	e, ok := branches[name].(map[string]interface{})
	return e, ok
}

func localBranchExists(t *testing.T, env *TestEnv, branch string) bool {
	t.Helper()
	_, err := env.RunCommandAllowFail(t, env.HubPath, "git", "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// Without the flag the default branch stays put, exactly as before.
func TestMoveDefault_RefusedWithoutFlag(t *testing.T) {
	t.Parallel()
	env := setupDefaultMoveEnv(t)
	before, _ := os.ReadFile(filepath.Join(env.HubPath, "hop.json"))

	_, stderr, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "move", "main", "next")
	if code == 0 {
		t.Fatal("move of the default branch without the flag succeeded")
	}
	if !strings.Contains(stderr, "Cannot move the default branch 'main'") {
		t.Errorf("stderr = %q, want the default-branch refusal", stderr)
	}
	if !strings.Contains(stderr, "--default-branch") {
		t.Errorf("stderr = %q, want a hint naming --default-branch", stderr)
	}
	after, _ := os.ReadFile(filepath.Join(env.HubPath, "hop.json"))
	if string(before) != string(after) {
		t.Error("hop.json changed by a refused move")
	}
	if !localBranchExists(t, env, "main") || localBranchExists(t, env, "next") {
		t.Error("branches changed by a refused move")
	}
}

// --default-branch on a branch that is not the default is a mistake.
func TestMoveDefault_FlagOnNonDefaultRefused(t *testing.T) {
	t.Parallel()
	env := setupDefaultMoveEnv(t)

	_, stderr, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "move", "--default-branch", "feature/old", "feature/new")
	if code == 0 {
		t.Fatal("--default-branch on a non-default branch succeeded")
	}
	if !strings.Contains(stderr, "not the default branch") {
		t.Errorf("stderr = %q", stderr)
	}
	if !localBranchExists(t, env, "feature/old") {
		t.Error("feature/old vanished")
	}
}

// Branch, worktree dir, hop.json, state and `current` all agree after.
func TestMoveDefault_RenamesEverything(t *testing.T) {
	t.Parallel()
	env := setupDefaultMoveEnv(t)

	out := env.RunGitHopCombined(t, env.HubPath, "move", "--default-branch", "main", "next")
	if !localBranchExists(t, env, "next") || localBranchExists(t, env, "main") {
		t.Error("git branch not renamed main -> next")
	}
	if _, err := os.Stat(filepath.Join(env.HubPath, "hops", "main")); !os.IsNotExist(err) {
		t.Error("hops/main still exists")
	}
	nextPath := filepath.Join(env.HubPath, "hops", "next")
	if got := strings.TrimSpace(env.RunCommand(t, nextPath, "git", "branch", "--show-current")); got != "next" {
		t.Errorf("worktree at hops/next is on %q", got)
	}

	if got := hubDefaultBranch(t, env); got != "next" {
		t.Errorf("hop.json repo.defaultBranch = %q, want next", got)
	}
	if _, ok := hubBranchEntry(t, env, "main"); ok {
		t.Error("hop.json still has branches.main")
	}
	if e, ok := hubBranchEntry(t, env, "next"); !ok {
		t.Error("hop.json has no branches.next")
	} else if p, _ := e["path"].(string); !strings.HasSuffix(filepath.ToSlash(p), "hops/next") {
		t.Errorf("branches.next.path = %q", p)
	}

	target, err := os.Readlink(filepath.Join(env.HubPath, "current"))
	if err != nil {
		t.Fatal(err)
	}
	if !sameDir(filepath.Join(env.HubPath, target), nextPath) {
		t.Errorf("current -> %s, want %s", target, nextPath)
	}

	st, err := env.LoadState(t)
	if err != nil || st == nil {
		t.Fatalf("state: %v", err)
	}
	found := false
	for _, repo := range st.Repositories {
		if repo.DefaultBranch != "next" {
			t.Errorf("state defaultBranch = %q, want next", repo.DefaultBranch)
		}
		for _, wt := range repo.Worktrees {
			if wt.Branch == "main" {
				t.Errorf("state still has a main worktree: %s", wt.Path)
			}
			if wt.Branch == "next" && sameDir(wt.Path, nextPath) {
				found = true
			}
		}
	}
	if !found {
		t.Error("state has no worktree next at hops/next")
	}

	// Local only: the remote is not touched, and the output says what is left.
	remote := env.RunCommand(t, env.BareRepoPath, "git", "branch", "--list")
	if !strings.Contains(remote, "main") || strings.Contains(remote, "next") {
		t.Errorf("origin branches = %q, want main only", remote)
	}
	if !strings.Contains(out, "origin") {
		t.Errorf("output has no remote hint: %s", out)
	}
}

// hopspaceConfigsAgree reports whether every hopspace hop.json under the
// data home that records a default branch records want.
func hopspaceConfigsAgree(t *testing.T, env *TestEnv, want string) bool {
	t.Helper()
	ok, seen := true, 0
	defer func() {
		if seen == 0 {
			t.Error("no hopspace config records a default branch: the check proves nothing")
		}
	}()
	_ = filepath.Walk(filepath.Join(env.RootDir, "data"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Name() != "hop.json" {
			return nil
		}
		data, _ := os.ReadFile(p)
		var cfg struct {
			Repo struct {
				DefaultBranch string `json:"defaultBranch"`
			} `json:"repo"`
		}
		if json.Unmarshal(data, &cfg) == nil && cfg.Repo.DefaultBranch != "" {
			seen++
		}
		if cfg.Repo.DefaultBranch != "" && cfg.Repo.DefaultBranch != want {
			t.Logf("%s records defaultBranch %q", p, cfg.Repo.DefaultBranch)
			ok = false
		}
		return nil
	})
	return ok
}

// Worktrees whose recorded base is the old default follow the rename, and
// so does the hub-wide compare branch.
func TestMoveDefault_RecordedBasesFollow(t *testing.T) {
	t.Parallel()
	env := setupDefaultMoveEnv(t)
	hub := filepath.Join(env.HubPath, "hop.json")
	data, _ := os.ReadFile(hub)
	var cfg map[string]interface{}
	_ = json.Unmarshal(data, &cfg)
	branches := cfg["branches"].(map[string]interface{})
	branches["feature/old"].(map[string]interface{})["base"] = "main"
	cfg["settings"] = map[string]interface{}{"compareBranch": "main"}
	out, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(hub, out, 0o644); err != nil {
		t.Fatal(err)
	}

	env.RunGitHop(t, env.HubPath, "move", "--default-branch", "main", "next")

	got, _ := hubBranchEntry(t, env, "feature/old")
	if got["base"] != "next" {
		t.Errorf("feature/old base = %v, want next", got["base"])
	}
	settings, _ := readHubJSON(t, env)["settings"].(map[string]interface{})
	if settings["compareBranch"] != "next" {
		t.Errorf("settings.compareBranch = %v, want next", settings["compareBranch"])
	}
}

// The one-argument form renames the worktree the shell is in.
func TestMoveDefault_OneArgFromInsideWorktree(t *testing.T) {
	t.Parallel()
	env := setupDefaultMoveEnv(t)

	env.RunGitHop(t, filepath.Join(env.HubPath, "hops", "main"), "move", "--default-branch", "next")

	if hubDefaultBranch(t, env) != "next" || !localBranchExists(t, env, "next") {
		t.Error("one-argument rename did not take")
	}
}

// --dry-run reports what the run produces and writes nothing.
func TestMoveDefault_DryRunMatchesRun(t *testing.T) {
	t.Parallel()
	env := setupDefaultMoveEnv(t)
	before, _ := os.ReadFile(filepath.Join(env.HubPath, "hop.json"))

	var dry, run map[string]interface{}
	if err := json.Unmarshal([]byte(env.RunGitHop(t, env.HubPath, "move", "--default-branch", "--dry-run", "--json", "main", "next")), &dry); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(env.HubPath, "hop.json"))
	if string(before) != string(after) || !localBranchExists(t, env, "main") || localBranchExists(t, env, "next") {
		t.Fatal("dry-run changed something")
	}
	if _, err := os.Stat(filepath.Join(env.HubPath, "hops", "next")); !os.IsNotExist(err) {
		t.Fatal("dry-run created hops/next")
	}

	if err := json.Unmarshal([]byte(env.RunGitHop(t, env.HubPath, "move", "--default-branch", "--json", "main", "next")), &run); err != nil {
		t.Fatal(err)
	}
	if dry["dry_run"] != true {
		t.Errorf("dry result lacks dry_run: %v", dry)
	}
	delete(dry, "dry_run")
	a, _ := json.Marshal(dry)
	b, _ := json.Marshal(run)
	if string(a) != string(b) {
		t.Errorf("dry-run result %s != run result %s", a, b)
	}
	if run["current_updated"] != true || run["default_branch"] != true {
		t.Errorf("run result = %v", run)
	}
}

// A dry run refuses what the run would.
func TestMoveDefault_DryRunRefusesLikeRun(t *testing.T) {
	t.Parallel()
	env := setupDefaultMoveEnv(t)
	env.RunCommand(t, env.HubPath, "git", "branch", "next", "main")

	for _, args := range [][]string{
		{"move", "--default-branch", "main", "next"},
		{"move", "--default-branch", "--dry-run", "main", "next"},
	} {
		_, _, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, args...)
		if code == 0 {
			t.Errorf("%v succeeded onto an existing branch", args)
		}
	}
	if hubDefaultBranch(t, env) != "main" {
		t.Error("hop.json changed by a refused move")
	}
}

// A hub worktree already named after the target is refused too.
func TestMoveDefault_TargetIsHubBranchRefused(t *testing.T) {
	t.Parallel()
	env := setupDefaultMoveEnv(t)

	_, _, code := env.RunCommandWithExit(t, env.HubPath, env.BinPath, "move", "--default-branch", "main", "feature/old")
	if code == 0 {
		t.Fatal("renamed onto a branch the hub tracks")
	}
}

// Uncommitted work moves with the worktree, as for any move.
func TestMoveDefault_DirtyWorktreeTravels(t *testing.T) {
	t.Parallel()
	env := setupDefaultMoveEnv(t)
	WriteFile(t, filepath.Join(env.HubPath, "hops", "main", "scratch.txt"), "wip")

	env.RunGitHop(t, env.HubPath, "move", "--default-branch", "main", "next")

	data, err := os.ReadFile(filepath.Join(env.HubPath, "hops", "next", "scratch.txt"))
	if err != nil || string(data) != "wip" {
		t.Errorf("scratch.txt at new path: %q, %v", data, err)
	}
}

// `current` stays where it was when it pointed elsewhere.
func TestMoveDefault_CurrentElsewhereUntouched(t *testing.T) {
	t.Parallel()
	env := setupDefaultMoveEnv(t)
	pointCurrentAt(t, env, "feature/old")

	env.RunGitHop(t, env.HubPath, "move", "--default-branch", "main", "next")

	target, _ := os.Readlink(filepath.Join(env.HubPath, "current"))
	if filepath.ToSlash(target) != "hops/feature/old" {
		t.Errorf("current -> %s", target)
	}
}

// git-flow's recorded bases naming the old default follow the rename.
func TestMoveDefault_GitflowBasesFollow(t *testing.T) {
	t.Parallel()
	e := setupGitflowEnv(t)
	e.enable(t)
	e.gitHop(t, "add", "feature/a")
	e.RunCommand(t, e.HubPath, "git", "config", "gitflow.branch.feature/a.base", "main")

	e.gitHop(t, "move", "--default-branch", "main", "next")

	got := strings.TrimSpace(e.RunCommand(t, e.HubPath, "git", "config", "gitflow.branch.feature/a.base"))
	if got != "next" {
		t.Errorf("gitflow.branch.feature/a.base = %q, want next", got)
	}
}

func sameDir(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// With the hopspace in the data home (a global hub), its record of the
// default branch follows the rename too.
func TestMoveDefault_GlobalHopspaceFollows(t *testing.T) {
	t.Parallel()
	env := SetupTestEnv(t)
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "Initial commit")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub", "--global")
	if !hopspaceConfigsAgree(t, env, "main") {
		t.Fatal("hopspace does not start on main")
	}

	env.RunGitHop(t, env.HubPath, "move", "--default-branch", "main", "next")

	if !hopspaceConfigsAgree(t, env, "next") {
		t.Error("a hopspace config still names main as default branch")
	}
	if hubDefaultBranch(t, env) != "next" {
		t.Error("hop.json not updated")
	}
}
