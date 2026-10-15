package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// startPointHub is a hub whose origin is one commit ahead of its local
// default branch, with an integration branch feat/int one commit ahead
// of that local default branch and checked out at hops/feat/int. Every
// candidate start-point is a different commit, so the tip a new branch
// starts at says which one add picked.
type startPointHub struct {
	env       *TestEnv
	localMain string // local main: the clone-time tip
	originTip string // origin's main, one commit later
	intTip    string // feat/int: local main plus one commit
	intPath   string
}

func setupStartPointHub(t *testing.T) *startPointHub {
	t.Helper()
	env := SetupTestEnv(t)
	env.RunCommand(t, env.RootDir, "git", "init", "--bare", env.BareRepoPath)
	env.RunCommand(t, env.RootDir, "git", "clone", env.BareRepoPath, env.SeedRepoPath)
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "init")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")
	env.RunGitHop(t, env.RootDir, env.BareRepoPath, "hub")

	h := &startPointHub{env: env}
	h.localMain = h.rev(t, env.HubPath, "refs/heads/main")

	env.RunGitHop(t, env.HubPath, "add", "feat/int", "--from", "main", "--no-fetch")
	h.intPath = filepath.Join(env.HubPath, "hops", "feat", "int")
	env.RunCommand(t, h.intPath, "git", "commit", "--allow-empty", "-m", "int")
	h.intTip = h.rev(t, h.intPath, "HEAD")

	// origin moves on after the clone; a fetch brings it to origin/main.
	env.RunCommand(t, env.SeedRepoPath, "git", "commit", "--allow-empty", "-m", "second")
	env.RunCommand(t, env.SeedRepoPath, "git", "push", "origin", "main")
	h.originTip = h.rev(t, env.SeedRepoPath, "HEAD")
	return h
}

func (h *startPointHub) rev(t *testing.T, dir, ref string) string {
	t.Helper()
	return strings.TrimSpace(h.env.RunCommand(t, dir, "git", "rev-parse", ref))
}

// tipOf is the commit the worktree of branch has checked out.
func (h *startPointHub) tipOf(t *testing.T, branch string) string {
	t.Helper()
	return h.rev(t, filepath.Join(h.env.HubPath, "hops", branch), "HEAD")
}

func (h *startPointHub) name(t *testing.T, sha string) string {
	switch sha {
	case h.localMain:
		return "local main"
	case h.originTip:
		return "origin/main"
	case h.intTip:
		return "feat/int"
	}
	return sha
}

func (h *startPointHub) assertTip(t *testing.T, branch, want string) {
	t.Helper()
	if got := h.tipOf(t, branch); got != want {
		t.Errorf("%s starts at %s, want %s", branch, h.name(t, got), h.name(t, want))
	}
}

// Inside a worktree, a new branch starts from that worktree's branch.
func TestAddStartPoint_InsideWorktreeUsesItsBranch(t *testing.T) {
	t.Parallel()
	h := setupStartPointHub(t)
	h.env.RunGitHop(t, h.intPath, "add", "feat/child")
	h.assertTip(t, "feat/child", h.intTip)
}

// From a subdirectory of a worktree too.
func TestAddStartPoint_WorktreeSubdirUsesItsBranch(t *testing.T) {
	t.Parallel()
	h := setupStartPointHub(t)
	sub := filepath.Join(h.intPath, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	h.env.RunGitHop(t, sub, "add", "feat/child")
	h.assertTip(t, "feat/child", h.intTip)
}

// At the hub root, a new branch starts from the local default branch,
// not from origin's.
func TestAddStartPoint_HubRootUsesLocalDefaultBranch(t *testing.T) {
	t.Parallel()
	h := setupStartPointHub(t)
	h.env.RunGitHop(t, h.env.HubPath, "add", "feat/root")
	h.assertTip(t, "feat/root", h.localMain)
}

// hop.add.defaultStartPoint=origin-default keeps the earlier default:
// origin's default branch, fetched first.
func TestAddStartPoint_ConfigOriginDefault(t *testing.T) {
	t.Parallel()
	h := setupStartPointHub(t)
	h.env.RunCommand(t, h.env.HubPath, "git", "config", "hop.add.defaultStartPoint", "origin-default")
	h.env.RunGitHop(t, h.intPath, "add", "feat/child")
	h.assertTip(t, "feat/child", h.originTip)
}

// hop.add.defaultStartPoint=default-branch ignores the worktree's branch.
func TestAddStartPoint_ConfigDefaultBranch(t *testing.T) {
	t.Parallel()
	h := setupStartPointHub(t)
	h.env.RunCommand(t, h.env.HubPath, "git", "config", "hop.add.defaultStartPoint", "default-branch")
	h.env.RunGitHop(t, h.intPath, "add", "feat/child")
	h.assertTip(t, "feat/child", h.localMain)
}

// --from wins over the configured default.
func TestAddStartPoint_FromFlagWins(t *testing.T) {
	t.Parallel()
	h := setupStartPointHub(t)
	h.env.RunCommand(t, h.env.HubPath, "git", "config", "hop.add.defaultStartPoint", "origin-default")
	h.env.RunGitHop(t, h.intPath, "add", "feat/child", "--from", "main")
	h.assertTip(t, "feat/child", h.localMain)
}

// --from takes the same keywords as the config key.
func TestAddStartPoint_FromCwdAtHubRootFallsBackToDefault(t *testing.T) {
	t.Parallel()
	h := setupStartPointHub(t)
	h.env.RunGitHop(t, h.env.HubPath, "add", "feat/root", "--from", "cwd")
	h.assertTip(t, "feat/root", h.localMain)
}

// GIT_HOP_ADD_FROM sits between --from and the config key.
func TestAddStartPoint_EnvBeatsConfig(t *testing.T) {
	t.Parallel()
	h := setupStartPointHub(t)
	h.env.RunCommand(t, h.env.HubPath, "git", "config", "hop.add.defaultStartPoint", "origin-default")
	cmd := exec.Command(h.env.BinPath, "add", "feat/child")
	cmd.Dir = h.intPath
	cmd.Env = append(append([]string{}, h.env.EnvVars...), "GIT_HOP_ADD_FROM=cwd")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	h.assertTip(t, "feat/child", h.intTip)
}

// --dry-run names the resolved start-point and why it was picked.
func TestAddStartPoint_DryRunExplains(t *testing.T) {
	t.Parallel()
	h := setupStartPointHub(t)

	out := h.env.RunGitHopCombined(t, h.intPath, "add", "feat/child", "--dry-run")
	for _, want := range []string{"[dry-run] Start-point: branch 'feat/int'", "current branch of the worktree", "hop.add.defaultStartPoint=cwd"} {
		if !strings.Contains(out, want) {
			t.Errorf("inside a worktree: dry-run output lacks %q:\n%s", want, out)
		}
	}

	out = h.env.RunGitHopCombined(t, h.env.HubPath, "add", "feat/root", "--dry-run")
	for _, want := range []string{"[dry-run] Start-point: default branch 'main'", "not inside a worktree"} {
		if !strings.Contains(out, want) {
			t.Errorf("hub root: dry-run output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Would fetch origin") {
		t.Errorf("hub root: a local start-point needs no fetch:\n%s", out)
	}

	out = h.env.RunGitHopCombined(t, h.intPath, "add", "feat/child", "--dry-run", "--from", "origin-default")
	for _, want := range []string{"[dry-run] Would fetch origin", "origin's default branch", "--from origin-default"} {
		if !strings.Contains(out, want) {
			t.Errorf("--from origin-default: dry-run output lacks %q:\n%s", want, out)
		}
	}
}

// Inside a worktree on a detached HEAD, a new branch starts at the
// detached commit, as git switch -c would; it records no base branch, and
// the ignored files it is seeded with come from that worktree.
func TestAddStartPoint_DetachedHeadUsesItsCommit(t *testing.T) {
	t.Parallel()
	h := setupStartPointHub(t)
	WriteFile(t, filepath.Join(h.intPath, ".gitignore"), "local.env\n")
	h.env.RunCommand(t, h.intPath, "git", "add", ".gitignore")
	h.env.RunCommand(t, h.intPath, "git", "commit", "-m", "ignore local.env")
	WriteFile(t, filepath.Join(h.intPath, "local.env"), "FROM=int\n")
	h.env.RunCommand(t, h.intPath, "git", "checkout", "--detach")
	// A commit on the detached HEAD: feat/int's tip is not where HEAD is.
	h.env.RunCommand(t, h.intPath, "git", "commit", "--allow-empty", "-m", "detached work")
	detached := h.rev(t, h.intPath, "HEAD")

	out := h.env.RunGitHopCombined(t, h.intPath, "add", "feat/child", "--dry-run")
	for _, want := range []string{"[dry-run] Start-point: '" + detached + "'", "the detached HEAD of the worktree"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output lacks %q:\n%s", want, out)
		}
	}

	h.env.RunGitHop(t, h.intPath, "add", "feat/child")
	if got := h.tipOf(t, "feat/child"); got != detached {
		t.Errorf("feat/child starts at %s, want the detached commit %s", h.name(t, got), detached)
	}
	if data, err := os.ReadFile(filepath.Join(h.env.HubPath, "hops", "feat", "child", "local.env")); err != nil || string(data) != "FROM=int\n" {
		t.Errorf("local.env not seeded from the detached worktree: %q, %v", data, err)
	}
	hopJSON, err := os.ReadFile(filepath.Join(h.env.HubPath, "hop.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Branches map[string]map[string]any `json:"branches"`
	}
	if err := json.Unmarshal(hopJSON, &cfg); err != nil {
		t.Fatal(err)
	}
	if base, ok := cfg.Branches["feat/child"]["base"]; ok {
		t.Errorf("feat/child records base %v; a detached start-point names no branch", base)
	}
}
