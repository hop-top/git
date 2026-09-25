package hop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
)

// hopspaceRecording returns a hopspace that records each branch at the
// given path, as an earlier release wrote a shared --global hopspace:
// one path per branch name, whichever hub registered it last.
func hopspaceRecording(path string, branches map[string]string) *Hopspace {
	cfg := &config.HopspaceConfig{Branches: map[string]config.HopspaceBranch{}}
	for b, p := range branches {
		cfg.Branches[b] = config.HopspaceBranch{Path: p, Exists: true}
	}
	return &Hopspace{Path: path, Config: cfg}
}

// findBase keeps every git command of an add in the hub's own
// repository, whatever the hopspace records.
func TestFindBase(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := NewWorktreeManager(afero.NewOsFs(), git.New())

	seed := filepath.Join(dir, "seed")
	runGit(t, "init", "-q", "-b", "main", seed)
	runGit(t, "-C", seed, "commit", "-q", "--allow-empty", "-m", "one")

	g1 := filepath.Join(dir, "g1", "app")
	g2 := filepath.Join(dir, "g2", "app")
	for _, hub := range []string{g1, g2} {
		runGit(t, "clone", "-q", "--bare", seed, hub)
		runGit(t, "-C", hub, "worktree", "add", "-q", "hops/main", "main")
	}
	runGit(t, "-C", g2, "worktree", "add", "-q", "-b", "feat", "hops/feat")
	// Listed before main, but gone from disk: never the base.
	runGit(t, "-C", g1, "worktree", "add", "-q", "-b", "gone", "hops/gone")
	if err := os.RemoveAll(filepath.Join(g1, "hops", "gone")); err != nil {
		t.Fatal(err)
	}
	g2Main := filepath.Join(g2, "hops", "main")

	empty := filepath.Join(dir, "empty")
	runGit(t, "clone", "-q", "--bare", seed, empty)

	reg := filepath.Join(dir, "reg")
	runGit(t, "clone", "-q", seed, reg)

	dotBare := filepath.Join(dir, "dotbare")
	runGit(t, "clone", "-q", "--bare", seed, filepath.Join(dotBare, ".git"))
	runGit(t, "-C", dotBare, "worktree", "add", "-q", "hops/main", "main")

	// Another hub's worktrees, all the shared hopspace knows.
	shared := hopspaceRecording(filepath.Join(dir, "data"), map[string]string{
		"main": g2Main,
		"feat": filepath.Join(g2, "hops", "feat"),
	})

	for _, tc := range []struct {
		name, hub, addDir string
		bases             []string // any one of them
	}{
		{"bare hub, hopspace records only another hub", g1, g1, []string{filepath.Join(g1, "hops", "main")}},
		{"the other hub keeps its own", g2, g2, []string{g2Main, filepath.Join(g2, "hops", "feat")}},
		{"bare hub without worktrees", empty, empty, []string{empty}},
		{"--regular hub", reg, reg, []string{reg}},
		{"bare repository kept in .git", dotBare, filepath.Join(dotBare, ".git"), []string{filepath.Join(dotBare, "hops", "main")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := m.findBase(shared, tc.hub)
			baseOK := false
			for _, b := range tc.bases {
				baseOK = baseOK || samePath(got.base, b)
			}
			if !baseOK || !samePath(got.addDir, tc.addDir) {
				t.Fatalf("findBase(%s) = %+v, want base in %v, addDir %s", tc.hub, got, tc.bases, tc.addDir)
			}
		})
	}

	// A fork's hopspace is no repository: its worktrees are. Only one
	// recorded under it is used, never one elsewhere.
	t.Run("not a repository: a worktree recorded under it", func(t *testing.T) {
		fork := filepath.Join(dir, "fork")
		forkMain := filepath.Join(fork, "main")
		runGit(t, "clone", "-q", seed, forkMain)
		hs := hopspaceRecording(fork, map[string]string{"main": forkMain})
		if got := m.findBase(hs, fork); got.base != forkMain || got.addDir != forkMain {
			t.Fatalf("findBase = %+v, want %s for both", got, forkMain)
		}
		hs = hopspaceRecording(fork, map[string]string{"main": g2Main})
		if got := m.findBase(hs, fork); got.base != fork {
			t.Fatalf("findBase = %+v, want the hopspace itself, not %s", got, g2Main)
		}
	})

	t.Run("a directory inside a repository is not its root", func(t *testing.T) {
		if repo, ok := hubRepository(git.New(), filepath.Join(g2, "hops")); ok {
			t.Fatalf("hubRepository = %s, want none", repo)
		}
		if repo, ok := hubRepository(git.New(), g2Main); ok {
			t.Fatalf("hubRepository(linked worktree) = %s, want none", repo)
		}
	})
}

// In a fork's hopspace, git runs in a worktree recorded under it that is
// on disk. One recorded but gone (moved away, as a fork-attach refusal's
// hint says to, or removed with git) cannot run anything; a walk of the
// branch map picked one at random, so an attach failed now and then.
// The order is fixed: the default branch first, then by name.
func TestHopspaceWorktreeIn_PicksPresentWorktreeInFixedOrder(t *testing.T) {
	fs := afero.NewMemMapFs()
	hsPath := "/data/forker/repo"
	hs := &Hopspace{Config: &config.HopspaceConfig{
		Repo:     config.RepoConfig{DefaultBranch: "feat"},
		Branches: map[string]config.HopspaceBranch{"feat": {Path: filepath.Join(hsPath, "feat"), Exists: true}},
	}}
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("gone%02d", i)
		hs.Config.Branches[name] = config.HopspaceBranch{Path: filepath.Join(hsPath, "hops", name), Exists: true}
	}
	mustMkdir := func(p string) {
		t.Helper()
		if err := fs.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	want := func(expected, why string) {
		t.Helper()
		for i := 0; i < 10; i++ {
			if got := hopspaceWorktreeIn(fs, hs, hsPath); got != expected {
				t.Fatalf("hopspaceWorktreeIn = %s, want %s (%s)", got, expected, why)
			}
		}
	}

	mustMkdir(filepath.Join(hsPath, "feat"))
	want(filepath.Join(hsPath, "feat"), "the default branch's worktree first")

	if err := fs.RemoveAll(filepath.Join(hsPath, "feat")); err != nil {
		t.Fatal(err)
	}
	mustMkdir(filepath.Join(hsPath, "hops", "gone12"))
	mustMkdir(filepath.Join(hsPath, "hops", "gone07"))
	want(filepath.Join(hsPath, "hops", "gone07"), "else the first on disk by name")

	// One recorded outside the hopspace is never used, on disk or not.
	hs.Config.Branches["aaa"] = config.HopspaceBranch{Path: "/elsewhere/aaa", Exists: true}
	mustMkdir("/elsewhere/aaa")
	want(filepath.Join(hsPath, "hops", "gone07"), "never one outside the hopspace")

	// None on disk: the first recorded under it, in the same order.
	fs = afero.NewMemMapFs()
	want(filepath.Join(hsPath, "feat"), "none on disk")

	// The default branch's worktree wins over one whose name sorts first.
	hs.Config.Branches["another"] = config.HopspaceBranch{Path: filepath.Join(hsPath, "hops", "another"), Exists: true}
	mustMkdir(filepath.Join(hsPath, "hops", "another"))
	want(filepath.Join(hsPath, "hops", "another"), "the default branch's gone")
	mustMkdir(filepath.Join(hsPath, "feat"))
	want(filepath.Join(hsPath, "feat"), "the default branch first, whatever sorts ahead")
}

// MoveWorktree renames and moves in the hub's own repository, even when
// every other worktree hop.json records is in another hub's repository.
func TestMoveWorktree_RunsInHubRepository(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fs := afero.NewOsFs()
	seed := filepath.Join(dir, "seed")
	runGit(t, "init", "-q", "-b", "main", seed)
	runGit(t, "-C", seed, "commit", "-q", "--allow-empty", "-m", "one")

	g1 := filepath.Join(dir, "g1", "app")
	g2 := filepath.Join(dir, "g2", "app")
	for _, hub := range []string{g1, g2} {
		runGit(t, "clone", "-q", "--bare", seed, hub)
		runGit(t, "-C", hub, "worktree", "add", "-q", "hops/main", "main")
	}
	runGit(t, "-C", g1, "worktree", "add", "-q", "-b", "feat/x", "hops/feat/x")

	hub, err := CreateHub(fs, g1, seed, "org", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.Update(func(cfg *config.HubConfig) error {
		cfg.Branches = map[string]config.HubBranch{
			// What an earlier release could leave: main recorded in g2.
			"main":   {Path: filepath.Join(g2, "hops", "main"), HopspaceBranch: "main"},
			"feat/x": {Path: filepath.Join(g1, "hops", "feat/x"), HopspaceBranch: "feat/x"},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	hs, err := LoadHopspace(fs, g1)
	if err != nil {
		t.Fatal(err)
	}

	m := NewWorktreeManager(fs, git.New())
	if _, _, err := m.MoveWorktree(hs, hub, "feat/x", "feat/w", "{hubPath}/hops/{branch}", "org", "repo"); err != nil {
		t.Fatalf("MoveWorktree: %v", err)
	}
	runGit(t, "-C", g1, "rev-parse", "--verify", "--quiet", "refs/heads/feat/w")
	if ok, _ := afero.DirExists(fs, filepath.Join(g1, "hops", "feat", "w")); !ok {
		t.Error("worktree not moved to hops/feat/w")
	}
	if out := runGit(t, "-C", g2, "branch", "--list"); strings.Contains(out, "feat") {
		t.Errorf("g2 branches = %q, want no feat branch", out)
	}
}
