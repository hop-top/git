package cmd

import (
	"fmt"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
)

// resolveCopySource picks the worktree whose ignored local state should
// seed a newly-created worktree.
//
// `git hop add <branch>` forks the new branch from a start-point resolved by
// WorktreeManager (see worktree.go:resolveStartPoint): the tip of
// repo.defaultBranch by default, or whatever --from / GIT_HOP_ADD_FROM /
// hop.add.defaultStartPoint named. The ignored files we want are the ones
// sitting in the worktree that has that start-point branch checked out —
// that is the tree the user was working in and hand-copying from.
//
// Resolution order:
//  1. The hub branch whose name matches the resolved start-point branch.
//     This is the literal fork source when it happens to be checked out.
//  2. The hub's default branch, when the start-point resolves to it (the
//     "" / "default-branch" sentinels) or the start-point named no
//     checked-out branch.
//
// Returns "" when neither yields an existing directory. A start-point that
// is a raw SHA, a tag, or a branch with no worktree has no "source
// worktree" to speak of; guessing at an unrelated one would copy a
// stranger's .env into the new tree. The caller skips copying and says so.
func resolveCopySource(fs afero.Fs, hub *hop.Hub, startPoint string) string {
	var candidates []string

	switch startPoint {
	case "", hop.StartPointDefaultBranch, hop.StartPointInitial:
		// No specific branch named (or the root commit, which belongs to
		// no branch) — the default branch is the fork source.
	default:
		candidates = append(candidates, startPoint)
	}
	candidates = append(candidates, hub.Config.Repo.DefaultBranch)

	for _, name := range candidates {
		if name == "" {
			continue
		}
		b, ok := hub.Config.Branches[name]
		if !ok || b.Path == "" {
			continue
		}
		path := config.ResolveWorktreePath(b.Path, hub.Path)
		if exists, err := afero.DirExists(fs, path); err == nil && exists {
			return path
		}
	}
	return ""
}

// copyIgnoredIntoWorktree seeds dstWorktree with the git-ignored local
// state present in the fork source, subject to the size ceiling, the
// deps-managed skip list, the #-hop-# marker opt-out, and the no-overwrite
// rule.
//
// Every failure path here is non-fatal by design. A worktree that exists
// without some of its ignored files is far better than a failed create, so
// problems are reported through output.Warn and `git hop add` carries on.
// override, when non-nil, is an explicit --[no-]copy-ignored from the
// command line and wins over the hop.add.copyIgnored config key.
func copyIgnoredIntoWorktree(fs afero.Fs, g git.GitInterface, hub *hop.Hub, hopspacePath, dstWorktree, startPoint string, globalConfig *config.GlobalConfig, gc *config.GitConfig, override *bool) {
	enabled := gc.GetBoolOrDefault(config.KeyAddCopyIgnored)
	if override != nil {
		enabled = *override
	}
	if !enabled {
		return
	}

	srcWorktree := resolveCopySource(fs, hub, startPoint)
	if srcWorktree == "" {
		output.Warn("could not determine a source worktree to copy ignored files from; skipping (disable this message with 'git config %s false')",
			config.KeyAddCopyIgnored)
		return
	}
	if srcWorktree == dstWorktree {
		return
	}

	maxBytes := gc.GetSizeOrDefault(config.KeyAddCopyIgnoredMaxSize)

	// Ask the deps layer which paths it owns rather than re-deriving the
	// list here. Failure to construct it is not fatal: an empty managed
	// set plus the no-overwrite guard still avoids clobbering symlinks.
	var managed map[string]bool
	if dm, err := services.NewDepsManager(fs, hopspacePath, globalConfig); err == nil {
		managed = services.DepsManagedPaths(dm, srcWorktree)
	}

	res, err := services.CopyIgnored(fs, g, srcWorktree, dstWorktree, services.IgnoredCopyOptions{
		MaxBytes:    maxBytes,
		DepsManaged: managed,
	})
	if err != nil {
		output.Warn("could not copy ignored files: %v", err)
		return
	}

	for _, w := range res.Warnings {
		output.Warn("copy ignored: %s", w)
	}

	// Skips must never be silent — a user who does not see the message
	// cannot know what was left behind or how to change it.
	for _, s := range res.Skipped {
		switch s.Reason {
		case services.SkipTooLarge:
			output.Warn("skipped %s (%s, over the %s limit); copy it manually or raise the limit with 'git config %s <size>'",
				s.Path, humanBytes(s.Bytes), humanBytes(maxBytes), config.KeyAddCopyIgnoredMaxSize)
		case services.SkipDepsManaged:
			output.Debug("skipped %s (managed by the dependency layer)", s.Path)
		case services.SkipExists:
			output.Debug("skipped %s (already present in the new worktree)", s.Path)
		case services.SkipMarked:
			output.Debug("skipped %s (ignore rule marked %s)", s.Path, services.IgnoredCopyMarker)
		}
	}

	if len(res.Copied) > 0 {
		output.Info("Copied %d ignored %s from %s", len(res.Copied),
			pluralEntries(len(res.Copied)), srcWorktree)
		if output.IsVerbose() {
			for _, p := range res.Copied {
				output.Debug("copied %s", p)
			}
		}
	}
}

// copyIgnoredOverride collapses the --[no-]copy-ignored pair into a single
// tri-state: nil means neither flag was given, so hop.add.copyIgnored
// decides. --no-copy-ignored wins over --copy-ignored when both appear,
// matching git's habit of letting the more restrictive request stand.
func copyIgnoredOverride(cmd *cobra.Command, yes, no bool) *bool {
	if cmd == nil {
		return nil
	}
	if cmd.Flags().Changed("no-copy-ignored") && no {
		off := false
		return &off
	}
	if cmd.Flags().Changed("copy-ignored") {
		v := yes
		return &v
	}
	return nil
}

// pluralEntries returns the correctly-pluralised noun for a count.
func pluralEntries(n int) string {
	if n == 1 {
		return "entry"
	}
	return "entries"
}

// humanBytes renders a byte count with a git-style size suffix, so the
// number in a skip message can be pasted straight back into
// `git config hop.add.copyIgnoredMaxSize`.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fK", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
