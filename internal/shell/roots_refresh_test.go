package shell_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/shell"
)

// hubWith builds an in-memory hub whose branches map to hub-relative
// worktree paths, matching how hop.json records them.
func hubWith(hubPath string, branches ...string) *hop.Hub {
	m := make(map[string]config.HubBranch, len(branches))
	for _, b := range branches {
		m[b] = config.HubBranch{Path: filepath.Join("hops", b)}
	}
	return &hop.Hub{Path: hubPath, Config: &config.HubConfig{Branches: m}}
}

// Removing a worktree must make it invisible to the shell handler. Merge
// semantics cannot express that -- MergeRootsCache appends to whatever the
// cache already holds -- so a removal needs a rebuild that replaces the
// hub's own entries wholesale.
func TestRebuildRootsCache_ShrinksWhenAWorktreeIsRemoved(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fs := afero.NewOsFs()

	hubPath := filepath.FromSlash("/w/hub")
	gone := filepath.Join(hubPath, "hops", "feature")

	if err := shell.RebuildRootsCache(fs, hubWith(hubPath, "main", "feature"), hubPath); err != nil {
		t.Fatalf("seed rebuild: %v", err)
	}
	if got := shell.ReadRootsCache(fs); shell.LookupRoot(got, gone) != gone {
		t.Fatalf("seed did not register the feature worktree: %v", got)
	}

	// The hub now knows only about main -- feature was removed.
	if err := shell.RebuildRootsCache(fs, hubWith(hubPath, "main"), hubPath); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	got := shell.ReadRootsCache(fs)
	if shell.LookupRoot(got, gone) != "" {
		t.Errorf("removed worktree survived the rebuild: %v", got)
	}
	wantMain := filepath.Join(hubPath, "hops", "main")
	if shell.LookupRoot(got, wantMain) != wantMain {
		t.Errorf("rebuild dropped a worktree that still exists: %v", got)
	}
}

// A rebuild is scoped to ONE hub. Entries belonging to other repositories
// are the whole reason the cache is merged rather than replaced, and a
// removal in repo A must not blind the handler to repo B.
func TestRebuildRootsCache_KeepsOtherReposEntries(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fs := afero.NewOsFs()

	other := filepath.FromSlash("/other/repo/hops/main")
	if err := shell.WriteRootsCache(fs, []string{other}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	hubPath := filepath.FromSlash("/w/hub")
	if err := shell.RebuildRootsCache(fs, hubWith(hubPath, "main"), hubPath); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	got := shell.ReadRootsCache(fs)
	if shell.LookupRoot(got, other) != other {
		t.Errorf("rebuild dropped another repo's worktree: %v", got)
	}
}

// A hub whose last worktree is gone must leave nothing behind. This is the
// case MergeRootsCache short-circuits on (len(fresh) == 0 returns early),
// which would leave the removed entry cached forever.
func TestRebuildRootsCache_EmptyHubDropsAllItsEntries(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fs := afero.NewOsFs()

	hubPath := filepath.FromSlash("/w/hub")
	only := filepath.Join(hubPath, "hops", "main")
	if err := shell.RebuildRootsCache(fs, hubWith(hubPath, "main"), hubPath); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := shell.RebuildRootsCache(fs, hubWith(hubPath), hubPath); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if got := shell.ReadRootsCache(fs); shell.LookupRoot(got, only) != "" {
		t.Errorf("last worktree survived an empty rebuild: %v", got)
	}
}

// A worktree moved to a new path must be reachable at the new path and
// unreachable at the old one -- a move is a removal and an addition, and
// the merge path only ever expressed the addition half.
func TestRebuildRootsCache_MoveRetiresTheOldPath(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fs := afero.NewOsFs()

	hubPath := filepath.FromSlash("/w/hub")
	oldPath := filepath.Join(hubPath, "hops", "old")
	newPath := filepath.Join(hubPath, "hops", "new")

	if err := shell.RebuildRootsCache(fs, hubWith(hubPath, "old"), hubPath); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := shell.RebuildRootsCache(fs, hubWith(hubPath, "new"), hubPath); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	got := shell.ReadRootsCache(fs)
	if shell.LookupRoot(got, oldPath) != "" {
		t.Errorf("pre-move path survived: %v", got)
	}
	if shell.LookupRoot(got, newPath) != newPath {
		t.Errorf("post-move path was not registered: %v", got)
	}
}

// A nil hub is what every command path holds when it could not load
// hop.json. The refresh is best-effort, so it must be a silent no-op rather
// than a panic or an error that surfaces to the user.
func TestRebuildRootsCache_NilHubIsANoOp(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fs := afero.NewOsFs()

	seeded := filepath.FromSlash("/w/hub/hops/main")
	if err := shell.WriteRootsCache(fs, []string{seeded}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := shell.RebuildRootsCache(fs, nil, filepath.FromSlash("/w/hub")); err != nil {
		t.Errorf("nil hub returned %v, want nil", err)
	}
	if got := shell.ReadRootsCache(fs); shell.LookupRoot(got, seeded) != seeded {
		t.Errorf("nil hub disturbed the cache: %v", got)
	}
}

// READ SIDE.
//
// The cache file being correct is only half the fix: a shell that started
// before the write still holds the array it slurped at startup. The wrapper
// is the seam -- every `git hop` the user runs goes through it, in the very
// shell whose array is stale -- so it must reload after the binary returns.
func TestGenerateWrapperFunction_ReloadsRootsAfterEveryInvocation(t *testing.T) {
	for _, shellType := range []string{"bash", "zsh", "fish"} {
		t.Run(shellType, func(t *testing.T) {
			src := shell.GenerateWrapperFunction(shellType)

			if !strings.Contains(src, "__git_hop_reload_roots") {
				t.Fatalf("%s wrapper never reloads the roots array", shellType)
			}

			// The reload has to be reachable from the wrapper body, i.e.
			// after the binary ran -- not only at block-source time. The
			// call site is the indented bare word; the definition above it
			// is preceded by a keyword, so the indent tells them apart.
			wrapperAt := strings.Index(src, "command git hop")
			if wrapperAt == -1 {
				t.Fatal("wrapper does not invoke the binary")
			}
			callAt := strings.Index(src, "\n    __git_hop_reload_roots\n")
			if callAt == -1 {
				t.Fatal("wrapper body never calls the reload")
			}
			if callAt < wrapperAt {
				t.Error("reload is invoked before the binary runs")
			}
		})
	}
}

// The reload must never leak into the chdir handler: that runs on every
// prompt, and re-reading a file there is exactly the per-prompt I/O the
// whole cache design exists to avoid.
func TestGenerateChdirHandler_DoesNotReloadOnThePromptPath(t *testing.T) {
	for _, shellType := range []string{"bash", "zsh", "fish"} {
		src := shell.GenerateChdirHandler(shellType)
		if strings.Contains(src, "__git_hop_reload_roots") {
			t.Errorf("%s chdir handler reloads the cache on the prompt path", shellType)
		}
	}
}
