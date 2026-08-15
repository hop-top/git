package shell_test

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/shell"
)

func TestRootsCache_RoundTrip(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fs := afero.NewOsFs()

	want := []string{"/w/hub/hops/main", "/w/hub/hops/feat/x"}
	if err := shell.WriteRootsCache(fs, want); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := shell.ReadRootsCache(fs)
	if len(got) != len(want) {
		t.Fatalf("read %d roots, want %d: %v", len(got), len(want), got)
	}
	for _, w := range want {
		if shell.LookupRoot(got, w) != w {
			t.Errorf("root %q did not survive the round trip: %v", w, got)
		}
	}
}

// A missing cache is the normal state before the first hop. It must read as
// "no roots", never as an error the prompt path has to handle.
func TestRootsCache_AbsentReadsEmpty(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if got := shell.ReadRootsCache(afero.NewOsFs()); len(got) != 0 {
		t.Errorf("absent cache returned %v, want empty", got)
	}
}

// Duplicate entries would make the shell loop do redundant comparisons on
// every prompt, and accumulate without bound as MergeRootsCache re-adds the
// same hub on every hop.
func TestRootsCache_Deduplicates(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fs := afero.NewOsFs()

	if err := shell.WriteRootsCache(fs, []string{"/w/a", "/w/a", "/w/b", "/w/a"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := shell.ReadRootsCache(fs); len(got) != 2 {
		t.Errorf("got %d roots, want 2 after dedup: %v", len(got), got)
	}
}

// A newline inside a path would desync every subsequent line of the cache,
// silently corrupting detection rather than failing. Such paths are dropped.
func TestRootsCache_RejectsEmbeddedNewline(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fs := afero.NewOsFs()

	if err := shell.WriteRootsCache(fs, []string{"/w/ok", "/w/ev\nil"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := shell.ReadRootsCache(fs)
	if len(got) != 1 || got[0] != "/w/ok" {
		t.Errorf("got %v, want only [/w/ok]", got)
	}
}

func TestLookupRoot(t *testing.T) {
	roots := []string{"/w/alpha", "/w/alpha/nested", "/w/beta"}

	cases := []struct {
		name string
		dir  string
		want string
	}{
		{"exact match", "/w/alpha", "/w/alpha"},
		{"subdirectory", "/w/alpha/internal/pkg", "/w/alpha"},
		{"nested worktree wins", "/w/alpha/nested/deep", "/w/alpha/nested"},
		{"unrelated", "/elsewhere/project", ""},
		// The separator has to be part of the comparison, or a sibling
		// whose name merely extends a worktree's reads as being inside it.
		{"sibling sharing a prefix", "/w/alpha-scratch", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shell.LookupRoot(roots, tc.dir); got != tc.want {
				t.Errorf("LookupRoot(%q) = %q, want %q", tc.dir, got, tc.want)
			}
		})
	}
}

// HubWorktreeRoots must anchor relative recorded paths on the hub, the same
// way the switch path resolves them. Anchoring on the process cwd instead
// would emit paths that never match $PWD.
func TestHubWorktreeRoots_AnchorsRelativePathsOnHub(t *testing.T) {
	hubPath := filepath.FromSlash("/w/hub")
	hub := &hop.Hub{
		Path: hubPath,
		Config: &config.HubConfig{
			Branches: map[string]config.HubBranch{
				"main":    {Path: filepath.FromSlash("hops/main")},
				"outside": {Path: filepath.FromSlash("/elsewhere/wt")},
			},
		},
	}

	roots := shell.HubWorktreeRoots(hub, hubPath)
	if len(roots) != 2 {
		t.Fatalf("got %d roots, want 2: %v", len(roots), roots)
	}

	wantMain := filepath.Join(hubPath, "hops", "main")
	if shell.LookupRoot(roots, wantMain) != wantMain {
		t.Errorf("relative path was not anchored on the hub: %v", roots)
	}

	wantOutside := filepath.FromSlash("/elsewhere/wt")
	if shell.LookupRoot(roots, wantOutside) != wantOutside {
		t.Errorf("absolute path did not survive verbatim: %v", roots)
	}
}

// Merging rather than replacing is what lets the cache cover more than one
// repository: hopping in repo A must not blind the handler to repo B's
// worktrees, which the user may reach by plain cd.
func TestMergeRootsCache_KeepsOtherReposEntries(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fs := afero.NewOsFs()

	other := filepath.FromSlash("/other/repo/hops/main")
	if err := shell.WriteRootsCache(fs, []string{other}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	hubPath := filepath.FromSlash("/w/hub")
	hub := &hop.Hub{
		Path: hubPath,
		Config: &config.HubConfig{
			Branches: map[string]config.HubBranch{
				"main": {Path: filepath.FromSlash("hops/main")},
			},
		},
	}
	if err := shell.MergeRootsCache(fs, hub, hubPath); err != nil {
		t.Fatalf("merge: %v", err)
	}

	got := shell.ReadRootsCache(fs)
	if shell.LookupRoot(got, other) != other {
		t.Errorf("merge dropped another repo's worktree: %v", got)
	}
	wantNew := filepath.Join(hubPath, "hops", "main")
	if shell.LookupRoot(got, wantNew) != wantNew {
		t.Errorf("merge did not add the hub's worktree: %v", got)
	}
}
