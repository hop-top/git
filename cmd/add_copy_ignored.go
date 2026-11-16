package cmd

import (
	"fmt"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"hop.top/git/internal/cli"
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
// add (see resolveAddStartPoint): the branch of the worktree add runs in
// by default, else repo.defaultBranch, or whatever --from /
// GIT_HOP_ADD_FROM / hop.add.defaultStartPoint named. The ignored files we want are the ones
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

	startPoint = startPointBranch(startPoint)
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

// forkSource is the worktree the new one forks from: the one the
// start-point came from (cwd), else resolveCopySource's pick. "" when
// there is none.
func forkSource(fs afero.Fs, hub *hop.Hub, start addStartPoint) string {
	if start.Source != "" {
		return start.Source
	}
	return resolveCopySource(fs, hub, start.Spec)
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
// command line and wins over the hop.add.copyIgnored config key. The
// source is start.Source when the start-point came from a worktree (cwd),
// else resolveCopySource's pick.
func copyIgnoredIntoWorktree(fs afero.Fs, g git.GitInterface, hub *hop.Hub, hopspacePath, dstWorktree string, start addStartPoint, globalConfig *config.GlobalConfig, gc *config.GitConfig, override *bool) {
	runCopyIgnored(fs, g, hub, hopspacePath, dstWorktree, start, globalConfig, gc, override, false)
}

// previewCopyIgnored is the dry-run twin of copyIgnoredIntoWorktree: same
// source, same filters, same decisions, nothing written. It lists every
// entry the real run will copy and every one it will skip with the reason.
func previewCopyIgnored(fs afero.Fs, g git.GitInterface, hub *hop.Hub, hopspacePath, dstWorktree string, start addStartPoint, globalConfig *config.GlobalConfig, gc *config.GitConfig, override *bool) {
	runCopyIgnored(fs, g, hub, hopspacePath, dstWorktree, start, globalConfig, gc, override, true)
}

func runCopyIgnored(fs afero.Fs, g git.GitInterface, hub *hop.Hub, hopspacePath, dstWorktree string, start addStartPoint, globalConfig *config.GlobalConfig, gc *config.GitConfig, override *bool, dryRun bool) {
	enabled := gc.GetBoolOrDefault(config.KeyAddCopyIgnored)
	if override != nil {
		enabled = *override
	}
	if !enabled {
		if dryRun {
			output.Info("[dry-run] Would not copy ignored entries (disabled)")
		}
		return
	}

	srcWorktree := forkSource(fs, hub, start)
	if srcWorktree == "" {
		output.Warn("could not determine a source worktree to copy ignored files from; skipping (disable this message with 'git config %s false')",
			config.KeyAddCopyIgnored)
		return
	}
	if srcWorktree == dstWorktree && !dryRun {
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

	exclude, err := gc.GetAll(config.KeyAddCopyIgnoredExclude)
	if err != nil {
		output.Warn("could not read %s: %v", config.KeyAddCopyIgnoredExclude, err)
	}

	res, err := services.CopyIgnored(fs, g, srcWorktree, dstWorktree, services.IgnoredCopyOptions{
		MaxBytes:    maxBytes,
		DepsManaged: managed,
		Exclude:     exclude,
		DryRun:      dryRun,
	})
	if err != nil {
		output.Warn("could not copy ignored files: %v", err)
		return
	}

	for _, w := range res.Warnings {
		output.Warn("copy ignored: %s", w)
	}

	if dryRun {
		reportCopyIgnoredPreview(res, srcWorktree)
		return
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
		case services.SkipExcludedDefault, services.SkipExcludedConfig:
			output.Debug("skipped %s (%s)", s.Path, skipDescription(s))
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

// skipDescription says why an entry is skipped, in terms a user can act on.
func skipDescription(s services.IgnoredSkip) string {
	switch s.Reason {
	case services.SkipTooLarge:
		return fmt.Sprintf("%s, over the size limit: %s", humanBytes(s.Bytes), config.KeyAddCopyIgnoredMaxSize)
	case services.SkipDepsManaged:
		return "managed by the dependency layer"
	case services.SkipExists:
		return "already present in the new worktree"
	case services.SkipMarked:
		return "ignore rule marked " + services.IgnoredCopyMarker
	case services.SkipExcludedDefault:
		return "built-in default exclusion; opt back in with '!" + s.Path + "' in " + config.KeyAddCopyIgnoredExclude
	case services.SkipExcludedConfig:
		return config.KeyAddCopyIgnoredExclude
	}
	return string(s.Reason)
}

// reportCopyIgnoredPreview prints the dry-run listing: the entries the real
// run copies, then those it skips with the reason.
func reportCopyIgnoredPreview(res *services.IgnoredCopyResult, src string) {
	output.Info("[dry-run] Would copy %d ignored %s from %s", len(res.Copied),
		pluralEntries(len(res.Copied)), src)
	for _, p := range res.Copied {
		output.Info("[dry-run]   copy %s", p)
	}
	for _, s := range res.Skipped {
		output.Info("[dry-run]   skip %s (%s)", s.Path, skipDescription(s))
	}
}

// copyIgnoredOverride collapses the --[no-]copy-ignored pair into a single
// tri-state: nil means neither flag was given, so hop.add.copyIgnored
// decides. --no-copy-ignored wins over --copy-ignored when both appear,
// matching git's habit of letting the more restrictive request stand.
func copyIgnoredOverride(cmd *cobra.Command, yes, no bool) *bool {
	return cli.NegatableFlag(cmd, "copy-ignored", yes, no)
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
