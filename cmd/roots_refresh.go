package cmd

import (
	"github.com/spf13/afero"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/shell"
)

// refreshRootsCache restates the hub's worktree set in the shell
// integration's roots cache.
//
// That cache is the only thing standing between the chdir handler and a
// fork per prompt: it lets the handler answer "is $PWD a worktree?" with a
// string compare. It is also only as current as the last command that wrote
// it, and for a long time the only command that did was the explicit switch
// path. Every other way of changing which worktrees exist -- add, remove,
// move, clone -- left the cache describing a world that no longer existed,
// so a worktree created after the shell started was never detected and one
// removed kept being announced.
//
// A rebuild rather than a merge, because the interesting fact after a
// remove or a move is which path STOPPED being a worktree, and a cache that
// only grows cannot say it. Callers pass the hub as it stands after the
// mutation; on an add that set grew, on a remove it shrank, and the cache
// simply follows.
//
// Best-effort by contract. The operation this trails has already succeeded
// and is not undoable, so a cache write must never be the thing that fails
// it -- the worst a swallowed error costs is detection going stale until
// the next command, which is exactly the state the whole fix improves on.
func refreshRootsCache(fs afero.Fs, hub *hop.Hub, hubPath string) {
	if err := shell.RebuildRootsCache(fs, hub, hubPath); err != nil {
		output.Debug("failed to refresh worktree roots cache: %v", err)
	}
}
