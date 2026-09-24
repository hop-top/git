package cmd

import (
	"hop.top/git/internal/config"
	"hop.top/git/internal/output"
)

// checkRepositoryFormat warns about a hub whose config turns
// extensions.worktreeConfig on at core.repositoryformatversion 0, and
// under --fix sets the version to 1.
//
// Conversions made before git-hop raised the version with the extension
// left hubs this way. git honours worktreeConfig at version 0 for
// compatibility, so git itself works; implementations that read
// extensions only at version 1 (libgit2, JGit) miss the hub's
// per-worktree core.bare. Hence a warning, not an issue. Version 1 is
// what git sets when it turns the extension on, and every git that knows
// the extension reads it.
func checkRepositoryFormat(scope config.ConfigScope, opts doctorOpts, r *doctorReport) {
	key := config.KeyRepositoryFormatVersion
	stale, err := scope.WorktreeConfigAtFormatV0()
	if err != nil {
		output.Warn("Could not check the hub's repository format: %v", err)
		r.record(doctorKindWarning, doctorCheckConfig, key, "could not check the repository format: %v", err)
		return
	}
	if !stale {
		return
	}

	msg := "extensions.worktreeConfig is on at core.repositoryformatversion 0; " +
		"tools other than git may ignore the hub's per-worktree config"
	output.Warn("%s", msg)
	r.record(doctorKindWarning, doctorCheckConfig, key, "%s; run 'git hop doctor --fix' to set version 1", msg)

	switch {
	case !opts.fix:
		output.Hint("run 'git hop doctor --fix' to set %s to 1", key)
	case !opts.mutating():
		output.Info("[dry-run] Would set %s to 1", key)
		r.repaired(opts, doctorCheckConfig, key, "set to 1")
	default:
		if err := scope.SetRepositoryFormatVersion1(); err != nil {
			output.Error("Failed to set %s to 1: %v", key, err)
			r.failed(doctorCheckConfig, key, "set to 1: %v", err)
			return
		}
		output.Info("Set %s to 1", key)
		r.repaired(opts, doctorCheckConfig, key, "set to 1")
	}
}
