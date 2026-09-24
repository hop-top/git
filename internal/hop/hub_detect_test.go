package hop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

// bareHub clones a one-commit seed into <dir>/<name> as a bare repo and
// adds hops/main, the layout `git hop init` produces, without hop.json.
func bareHub(t *testing.T, dir, name string) string {
	t.Helper()
	seed := filepath.Join(dir, "seed")
	if _, err := os.Stat(seed); err != nil {
		mustRun(t, "git", "init", "-q", "-b", "main", seed)
		mustRun(t, "git", "-C", seed, "commit", "-q", "--allow-empty", "-m", "init")
	}
	hub := filepath.Join(dir, name)
	mustRun(t, "git", "clone", "-q", "--bare", seed, hub)
	mustRun(t, "git", "-C", hub, "worktree", "add", "-q", "hops/main", "main")
	return hub
}

func TestFindUnregisteredHub(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	g := git.New()

	cases := []struct {
		name  string
		setup func(t *testing.T) (start, want string)
	}{
		{"core.bare as clone writes it, from the root", func(t *testing.T) (string, string) {
			h := bareHub(t, dir, "plain")
			return h, h
		}},
		{"from inside a nested worktree path", func(t *testing.T) (string, string) {
			h := bareHub(t, dir, "nested")
			mustRun(t, "git", "-C", h, "worktree", "add", "-q", "-b", "feat/x", "hops/feat/x", "main")
			return filepath.Join(h, "hops", "feat", "x"), h
		}},
		{"bare=true, no spaces", func(t *testing.T) (string, string) {
			h := bareHub(t, dir, "nospace")
			writeConfigBare(t, h, "\tbare=true\n")
			return filepath.Join(h, "hops", "main"), h
		}},
		{"bare = yes", func(t *testing.T) (string, string) {
			h := bareHub(t, dir, "yes")
			mustRun(t, "git", "-C", h, "config", "core.bare", "yes")
			return h, h
		}},
		{"core.bare set through an include", func(t *testing.T) (string, string) {
			h := bareHub(t, dir, "incl")
			inc := filepath.Join(dir, "bare.gitconfig")
			if err := os.WriteFile(inc, []byte("[core]\n\tbare = true\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			mustRun(t, "git", "-C", h, "config", "--unset", "core.bare")
			mustRun(t, "git", "-C", h, "config", "include.path", inc)
			return h, h
		}},
		{"core.bare in config.worktree with extensions.worktreeConfig", func(t *testing.T) (string, string) {
			h := bareHub(t, dir, "wtcfg")
			mustRun(t, "git", "-C", h, "config", "extensions.worktreeConfig", "true")
			mustRun(t, "git", "-C", h, "config", "--unset", "core.bare")
			mustRun(t, "git", "-C", h, "config", "--file", filepath.Join(h, "config.worktree"), "core.bare", "true")
			return filepath.Join(h, "hops", "main"), h
		}},
		{"registered hub (hop.json) is not unregistered", func(t *testing.T) (string, string) {
			h := bareHub(t, dir, "registered")
			if err := os.WriteFile(filepath.Join(h, "hop.json"), []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}
			return h, ""
		}},
		{"regular repo with a stray hops/ dir", func(t *testing.T) (string, string) {
			r := filepath.Join(dir, "regular")
			mustRun(t, "git", "init", "-q", r)
			mustRun(t, "git", "-C", r, "commit", "-q", "--allow-empty", "-m", "init")
			mkdirAll(t, filepath.Join(r, "hops", "junk"))
			return filepath.Join(r, "hops", "junk"), ""
		}},
		{"regular repo's .git dir with a stray hops/ inside", func(t *testing.T) (string, string) {
			r := filepath.Join(dir, "gitdir")
			mustRun(t, "git", "init", "-q", r)
			mustRun(t, "git", "-C", r, "commit", "-q", "--allow-empty", "-m", "init")
			mkdirAll(t, filepath.Join(r, ".git", "hops", "junk"))
			return filepath.Join(r, ".git"), ""
		}},
		{"bare by shape but core.bare=false", func(t *testing.T) (string, string) {
			h := bareHub(t, dir, "notbare")
			mustRun(t, "git", "-C", h, "config", "core.bare", "false")
			return h, ""
		}},
		{"bare, hops/ not empty, no worktree registered under it", func(t *testing.T) (string, string) {
			seedHub := bareHub(t, dir, "tmp-stray")
			h := filepath.Join(dir, "stray")
			mustRun(t, "git", "clone", "-q", "--bare", seedHub, h)
			mustRun(t, "git", "-C", h, "worktree", "add", "-q", filepath.Join(dir, "elsewhere"), "main")
			mkdirAll(t, filepath.Join(h, "hops", "junk"))
			return h, ""
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, want := tc.setup(t)
			got, ok := hop.FindUnregisteredHub(fs, g, start)
			if want == "" {
				if ok {
					t.Fatalf("FindUnregisteredHub(%s) = %s, want no hub", start, got)
				}
				return
			}
			if !ok || got != want {
				t.Fatalf("FindUnregisteredHub(%s) = (%q, %v), want %q", start, got, ok, want)
			}
		})
	}
}

// countingGit fails the test if anything but the two probes the walk may
// use is called, and counts those.
type countingGit struct {
	git.GitInterface
	revParse, worktreeList int
}

func (c *countingGit) RevParse(string, ...string) (string, error) {
	c.revParse++
	return "", os.ErrNotExist
}

func (c *countingGit) WorktreeListPorcelain(string) (string, error) {
	c.worktreeList++
	return "", os.ErrNotExist
}

// The upward walk runs on every `git hop status` outside a hub, so it
// must not spawn git for directories that do not look like a bare hub.
func TestFindUnregisteredHub_PreCheckSpawnsNoGit(t *testing.T) {
	fs := afero.NewMemMapFs()
	mkdirAllFs(t, fs, "/a/b/c/d/e/f/g")
	// A hops/ dir alone, and a bare repo shape without hops/, are both
	// rejected before git is asked.
	mkdirAllFs(t, fs, "/a/b/hops/main")
	mkdirAllFs(t, fs, "/a/objects")
	mkdirAllFs(t, fs, "/a/refs")
	if err := afero.WriteFile(fs, "/a/HEAD", []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	g := &countingGit{}
	if root, ok := hop.FindUnregisteredHub(fs, g, "/a/b/c/d/e/f/g"); ok {
		t.Fatalf("found %s in a tree with no hub", root)
	}
	if g.revParse != 0 || g.worktreeList != 0 {
		t.Fatalf("git spawned for non-candidates: rev-parse %d, worktree list %d", g.revParse, g.worktreeList)
	}

	// A candidate that passes the structural check is asked about once.
	mkdirAllFs(t, fs, "/a/hops/main")
	hop.FindUnregisteredHub(fs, g, "/a/b/c")
	if g.revParse != 1 {
		t.Fatalf("rev-parse calls = %d, want 1 for the one candidate", g.revParse)
	}
}

func writeConfigBare(t *testing.T, repo, line string) {
	t.Helper()
	mustRun(t, "git", "-C", repo, "config", "--unset", "core.bare")
	f, err := os.OpenFile(filepath.Join(repo, "config"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("[core]\n" + line); err != nil {
		t.Fatal(err)
	}
}

func mkdirAll(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mkdirAllFs(t *testing.T, fs afero.Fs, p string) {
	t.Helper()
	if err := fs.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}
