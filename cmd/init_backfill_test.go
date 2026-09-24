package cmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/test/mocks"
)

// Test the worktree-list parser directly. The input shape is what
// `git worktree list --porcelain` emits: one record per blank-line-
// separated block, with key/value lines like "worktree <path>",
// "HEAD <sha>", "branch <ref>", or the bare marker "bare".
func TestParseWorktreeListPorcelain(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []porcelainWorktree
	}{
		{
			name: "empty input",
			in:   "",
			want: nil,
		},
		{
			name: "single bare entry — no branch",
			in: "worktree /repo\n" +
				"bare\n",
			want: []porcelainWorktree{
				{Path: "/repo", Bare: true},
			},
		},
		{
			name: "single branch worktree",
			in: "worktree /repo/hops/main\n" +
				"HEAD abc123\n" +
				"branch refs/heads/main\n",
			want: []porcelainWorktree{
				{Path: "/repo/hops/main", Branch: "main"},
			},
		},
		{
			name: "real-world bare + multiple branches (matches usp shape)",
			in: "worktree /repo\n" +
				"bare\n" +
				"\n" +
				"worktree /repo/hops/feat/a\n" +
				"HEAD aaa\n" +
				"branch refs/heads/feat/a\n" +
				"\n" +
				"worktree /repo/hops/main\n" +
				"HEAD bbb\n" +
				"branch refs/heads/main\n",
			want: []porcelainWorktree{
				{Path: "/repo", Bare: true},
				{Path: "/repo/hops/feat/a", Branch: "feat/a"},
				{Path: "/repo/hops/main", Branch: "main"},
			},
		},
		{
			name: "detached HEAD worktree — no branch line, must NOT be confused with bare",
			in: "worktree /repo/hops/detached\n" +
				"HEAD ccc\n" +
				"detached\n",
			want: []porcelainWorktree{
				{Path: "/repo/hops/detached", Detached: true},
			},
		},
		{
			name: "trailing blank lines tolerated",
			in: "worktree /repo\n" +
				"bare\n" +
				"\n" +
				"\n",
			want: []porcelainWorktree{
				{Path: "/repo", Bare: true},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseWorktreeListPorcelain(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d\ngot=%+v\nwant=%+v", len(got), len(tc.want), got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("entry %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// backfillHubConfigIfMissing is the integration unit. Given a hub path,
// it constructs a HubConfig from runtime git state (remote URL +
// worktree list + symbolic-ref HEAD) and writes it. If hop.json already
// exists, it must be a no-op.
//
// Test seams: MockGit.WorktreeListPorcelain reads from
// WorktreeListOut/Err; the other two git calls go through Runner.RunInDir
// keyed as "<dir>:git <args joined by space>".
func TestBackfillHubConfigIfMissing(t *testing.T) {
	const hubPath = "/repo"

	t.Run("writes hop.json when missing", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		_ = afero.WriteFile(fs, filepath.Join(hubPath, "HEAD"), []byte("ref: refs/heads/main\n"), 0644)

		m := mocks.NewMockGit()
		m.Runner.Responses[hubPath+":git remote get-url origin"] = "git@github.com:acme/usp.git\n"
		m.WorktreeListOut = "worktree /repo\nbare\n\n" +
			"worktree /repo/hops/main\nHEAD abc\nbranch refs/heads/main\n\n" +
			"worktree /repo/hops/feat/x\nHEAD def\nbranch refs/heads/feat/x\n"
		m.Runner.Responses[hubPath+":git symbolic-ref HEAD"] = "refs/heads/main\n"

		created, err := backfillHubConfigIfMissing(fs, m, hubPath)
		if err != nil {
			t.Fatalf("backfill returned error: %v", err)
		}
		if !created {
			t.Fatalf("backfill returned created=false; expected true")
		}

		loader := config.NewLoader(fs)
		cfg, err := loader.LoadHubConfig(hubPath)
		if err != nil {
			t.Fatalf("hop.json not loadable after backfill: %v", err)
		}
		if cfg.Repo.Org != "acme" || cfg.Repo.Repo != "usp" {
			t.Errorf("repo.org/repo = %q/%q, want acme/usp", cfg.Repo.Org, cfg.Repo.Repo)
		}
		if cfg.Repo.URI != "git@github.com:acme/usp.git" {
			t.Errorf("repo.uri = %q, want git@github.com:acme/usp.git", cfg.Repo.URI)
		}
		if cfg.Repo.DefaultBranch != "main" {
			t.Errorf("defaultBranch = %q, want main", cfg.Repo.DefaultBranch)
		}
		// The bare entry is skipped; only real worktrees are registered.
		if len(cfg.Branches) != 2 {
			t.Fatalf("branches count = %d, want 2 (got %+v)", len(cfg.Branches), cfg.Branches)
		}
		if b, ok := cfg.Branches["main"]; !ok || b.Path != "/repo/hops/main" || b.HopspaceBranch != "main" {
			t.Errorf("main entry wrong: %+v", b)
		}
		if b, ok := cfg.Branches["feat/x"]; !ok || b.Path != "/repo/hops/feat/x" || b.HopspaceBranch != "feat/x" {
			t.Errorf("feat/x entry wrong: %+v", b)
		}
		if len(cfg.Settings.EnvPatterns) == 0 {
			t.Errorf("envPatterns empty, want defaults populated")
		}
	})

	t.Run("no-op when hop.json already present", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		_ = afero.WriteFile(fs, filepath.Join(hubPath, "hop.json"), []byte(`{"repo":{"uri":"x","org":"x","repo":"x","defaultBranch":"main"},"branches":{},"settings":{"envPatterns":["dev"]}}`), 0644)

		m := mocks.NewMockGit()

		created, err := backfillHubConfigIfMissing(fs, m, hubPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if created {
			t.Errorf("created=true for existing hop.json; want false")
		}
		data, _ := afero.ReadFile(fs, filepath.Join(hubPath, "hop.json"))
		if !strings.Contains(string(data), `"envPatterns":["dev"]`) {
			t.Errorf("hop.json was rewritten unexpectedly:\n%s", data)
		}
	})

	t.Run("falls back to local path when no remote", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		hubPath := "/users/me/.w/labspace/myrepo"

		m := mocks.NewMockGit()
		m.Runner.Errors[hubPath+":git remote get-url origin"] = errors.New("no such remote")
		m.WorktreeListOut = "worktree " + hubPath + "\nbare\n\n" +
			"worktree " + hubPath + "/hops/main\nHEAD abc\nbranch refs/heads/main\n"
		m.Runner.Responses[hubPath+":git symbolic-ref HEAD"] = "refs/heads/main\n"

		created, err := backfillHubConfigIfMissing(fs, m, hubPath)
		if err != nil {
			t.Fatalf("backfill error with no remote: %v", err)
		}
		if !created {
			t.Fatalf("created=false; want true")
		}
		loader := config.NewLoader(fs)
		cfg, _ := loader.LoadHubConfig(hubPath)
		// Local-path fallback: repo from last path segment, org from
		// parent segment. Same fallback used by registerAsIs / mirror.
		if cfg.Repo.Repo != "myrepo" {
			t.Errorf("repo fallback = %q, want myrepo", cfg.Repo.Repo)
		}
		if cfg.Repo.Org != "labspace" {
			t.Errorf("org fallback = %q, want labspace", cfg.Repo.Org)
		}
	})

	t.Run("falls back to main when symbolic-ref fails", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		m := mocks.NewMockGit()
		m.Runner.Responses[hubPath+":git remote get-url origin"] = "git@github.com:a/b.git\n"
		m.WorktreeListOut = "worktree /repo\nbare\n\nworktree /repo/hops/dev\nHEAD x\nbranch refs/heads/dev\n"
		m.Runner.Errors[hubPath+":git symbolic-ref HEAD"] = errors.New("HEAD is detached")

		created, err := backfillHubConfigIfMissing(fs, m, hubPath)
		if err != nil {
			t.Fatalf("backfill error on missing HEAD: %v", err)
		}
		if !created {
			t.Fatalf("created=false; want true")
		}
		loader := config.NewLoader(fs)
		cfg, _ := loader.LoadHubConfig(hubPath)
		if cfg.Repo.DefaultBranch != "main" {
			t.Errorf("defaultBranch fallback = %q, want main", cfg.Repo.DefaultBranch)
		}
	})

	t.Run("propagates worktree-list error", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		m := mocks.NewMockGit()
		m.Runner.Responses[hubPath+":git remote get-url origin"] = "git@github.com:a/b.git\n"
		m.WorktreeListErr = errors.New("git unavailable")

		_, err := backfillHubConfigIfMissing(fs, m, hubPath)
		if err == nil {
			t.Fatalf("expected error when worktree list fails")
		}
	})
}

// resolveBackfillRoot picks the path to back-fill given the cwd and the
// repo structure detected there. BareWorktreeRoot/WorktreeRoot → cwd;
// WorktreeChild → the repository the worktree belongs to, as git reports
// its common dir (both the bare-hub "<hub>/worktrees/<n>" and the
// regular-hub "<hub>/.git/worktrees/<n>" shapes). Anything else →
// ("", false).
func TestResolveBackfillRoot(t *testing.T) {
	g := git.New()
	fs := afero.NewOsFs()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run := func(t *testing.T, args ...string) {
		t.Helper()
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	seed := filepath.Join(dir, "seed")
	run(t, "init", "-q", "-b", "main", seed)
	run(t, "-C", seed, "commit", "-q", "--allow-empty", "-m", "init")

	t.Run("BareWorktreeRoot returns cwd", func(t *testing.T) {
		got, ok := resolveBackfillRoot(fs, g, "/repo", config.BareWorktreeRoot)
		if !ok || got != "/repo" {
			t.Errorf("got (%q, %v), want (/repo, true)", got, ok)
		}
	})
	t.Run("WorktreeChild of a bare hub: gitdir is <hub>/worktrees/<name>", func(t *testing.T) {
		hub := filepath.Join(dir, "hub")
		run(t, "clone", "-q", "--bare", seed, hub)
		run(t, "-C", hub, "worktree", "add", "-q", "hops/main", "main")
		got, ok := resolveBackfillRoot(fs, g, filepath.Join(hub, "hops", "main"), config.WorktreeChild)
		if !ok || got != hub {
			t.Errorf("got (%q, %v), want (%s, true)", got, ok, hub)
		}
	})
	t.Run("WorktreeChild of a regular hub: gitdir is <hub>/.git/worktrees/<name>", func(t *testing.T) {
		// The returned root is the hub itself, not <hub>/.git. A regular
		// repository is a hub once a --regular conversion left hop.json.
		hub := filepath.Join(dir, "reg")
		run(t, "clone", "-q", seed, hub)
		if err := os.WriteFile(filepath.Join(hub, "hop.json"), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		wt := filepath.Join(dir, "reg-feature")
		run(t, "-C", hub, "worktree", "add", "-q", "-b", "feature", wt)
		got, ok := resolveBackfillRoot(fs, g, wt, config.WorktreeChild)
		if !ok || got != hub {
			t.Errorf("got (%q, %v), want (%s, true)", got, ok, hub)
		}
	})
	t.Run("WorktreeChild of a regular repo that is no hub declines", func(t *testing.T) {
		// Without hop.json the repository is not a hub: back-filling one
		// would register it without converting it.
		repo := filepath.Join(dir, "plain")
		run(t, "clone", "-q", seed, repo)
		wt := filepath.Join(dir, "plain-feature")
		run(t, "-C", repo, "worktree", "add", "-q", "-b", "feature", wt)
		if got, ok := resolveBackfillRoot(fs, g, wt, config.WorktreeChild); ok {
			t.Errorf("got (%q, true) for a linked worktree of a plain repository; want false", got)
		}
	})
	t.Run("WorktreeChild with a gitdir git does not accept returns false", func(t *testing.T) {
		wt := filepath.Join(dir, "wt")
		if err := os.MkdirAll(wt, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wt, ".git"),
			[]byte("gitdir: /some/random/dir/main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, ok := resolveBackfillRoot(fs, g, wt, config.WorktreeChild); ok {
			t.Errorf("got ok=true for a dangling gitdir; want false")
		}
	})
	t.Run("WorktreeRoot returns cwd (regular non-bare hub)", func(t *testing.T) {
		got, ok := resolveBackfillRoot(fs, g, "/repo", config.WorktreeRoot)
		if !ok || got != "/repo" {
			t.Errorf("got (%q, %v), want (/repo, true)", got, ok)
		}
	})
	t.Run("StandardRepo declines — not our case", func(t *testing.T) {
		if _, ok := resolveBackfillRoot(fs, g, "/repo", config.StandardRepo); ok {
			t.Errorf("got ok=true for StandardRepo; want false")
		}
	})
}
