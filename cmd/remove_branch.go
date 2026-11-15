package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// removeOptions are the choices of one removal that reach past the
// worktree: whether origin's branch goes too, and whether the local
// branch stays.
type removeOptions struct {
	DeleteRemote bool
	// KeepReason is why the local branch stays; empty means it goes.
	KeepReason string
}

// resolveRemoveKeepBranch decides whether remove leaves the local branch
// in place, and names what decided it. A typed --keep-branch or
// --keep-branch=false wins; otherwise hop.remove.keepBranch supplies the
// default; with neither, the branch goes with its worktree.
func resolveRemoveKeepBranch(cmd *cobra.Command, gc *config.GitConfig) string {
	if cmd.Flags().Changed("keep-branch") {
		if keep, _ := cmd.Flags().GetBool("keep-branch"); keep {
			return "--keep-branch"
		}
		return ""
	}
	if gc != nil && gc.GetBoolOrDefault(config.KeyRemoveKeepBranch) {
		return config.KeyRemoveKeepBranch
	}
	return ""
}

// removeOptionsFor assembles the options of a run from its flags and the
// config of the repository at dir.
func removeOptionsFor(cmd *cobra.Command, dir string, deleteRemote bool) removeOptions {
	return removeOptions{
		DeleteRemote: deleteRemote,
		KeepReason:   resolveRemoveKeepBranch(cmd, config.NewGitConfigIn(dir)),
	}
}

// resolveWorktreeBranch is the branch checked out in the worktree at
// worktreePath, read from git's worktree registry through basePath. It is
// the branch the worktree holds, which the hop.json key (fallback) may
// not be: a worktree switched to another branch, or a key that outlived a
// rename. The fallback stands when git does not list the worktree, lists
// it detached, or cannot be read.
func resolveWorktreeBranch(g git.GitInterface, basePath, worktreePath, fallback string) string {
	out, err := g.WorktreeListPorcelain(basePath)
	if err != nil {
		return fallback
	}
	for _, wt := range parseWorktreeListPorcelain(out) {
		if state.SamePath(wt.Path, worktreePath) {
			if wt.Branch != "" {
				return wt.Branch
			}
			break
		}
	}
	return fallback
}

// branchOutcome is what a removal does, or did, with the local branch of
// the worktree it removed.
type branchOutcome struct {
	// Branch is the branch looked at: the one the worktree had checked out.
	Branch string
	// Delete says the branch is to go (a plan) or went (a result).
	Delete bool
	// KeptReason says why the branch stays; empty when it goes.
	KeptReason string
	// Failed marks a branch that stays against the removal's intent (a
	// probe or delete that errored), as opposed to one the user or the
	// repository state decided to keep.
	Failed bool
}

// planBranchDeletion decides what removing the worktree does with its
// local branch, from the probes only. It deletes nothing, so the dry-run
// preview and the removal share it. It runs after the worktree is gone
// and git's worktree metadata pruned, or in a preview where git still
// lists the worktree as holding the branch: "checked out elsewhere" is
// then git's to report when the deletion is tried.
func planBranchDeletion(g git.GitInterface, basePath, branch, defaultBranch string, opts removeOptions) branchOutcome {
	out := branchOutcome{Branch: branch}
	switch {
	case branch == "":
		out.KeptReason = "worktree has no branch checked out"
	case branch == defaultBranch:
		out.KeptReason = "it is the default branch"
	case opts.KeepReason != "":
		out.KeptReason = "kept by " + opts.KeepReason
	default:
		exists, err := g.BranchExists(basePath, branch)
		switch {
		case err != nil:
			out.KeptReason = fmt.Sprintf("could not check whether it exists: %s", errorCause(err))
			out.Failed = true
		case !exists:
			out.KeptReason = "already absent"
		default:
			out.Delete = true
		}
	}
	return out
}

// firstLine is the first line of s.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// errorCause is the cause git gave for a failed command: the "error:" or
// "fatal:" line of its stderr, else the runner's first line.
func errorCause(err error) string {
	for _, line := range strings.Split(err.Error(), "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "git command failed: ")
		if i := strings.Index(line, "(stderr: "); i >= 0 {
			line = strings.TrimPrefix(line[i:], "(stderr: ")
		}
		for _, p := range []string{"error: ", "fatal: "} {
			if strings.HasPrefix(line, p) {
				return strings.TrimPrefix(line, p)
			}
		}
	}
	return firstLine(err.Error())
}

// applyBranchOutcome records the outcome on rec.
func applyBranchOutcome(rec *removeRecord, o branchOutcome) {
	rec.LocalBranch = o.Branch
	rec.BranchDeleted = o.Delete
	rec.BranchKeptReason = o.KeptReason
}

// unregisteredWorktree is a worktree git lists that hop.json does not.
type unregisteredWorktree struct {
	Path     string
	Branch   string
	Detached bool
	// Missing is a worktree whose directory is gone.
	Missing bool
}

// unregisteredWorktrees lists the worktrees of the hub at hubPath that git
// knows and hop.json does not: those made with plain `git worktree add`.
// The hub's bare repository is not one. A registry that cannot be read
// yields none.
func unregisteredWorktrees(fs afero.Fs, g git.GitInterface, hub *hop.Hub, hubPath string) []unregisteredWorktree {
	out, err := g.WorktreeListPorcelain(hubPath)
	if err != nil {
		return nil
	}
	registered := make([]string, 0, len(hub.Config.Branches))
	for _, bc := range hub.Config.Branches {
		if bc.Path != "" {
			registered = append(registered, config.ResolveWorktreePath(bc.Path, hubPath))
		}
	}
	var list []unregisteredWorktree
	for _, wt := range parseWorktreeListPorcelain(out) {
		if wt.Bare || state.SamePath(wt.Path, hubPath) || samePathAny(wt.Path, registered) {
			continue
		}
		// A branch hop.json lists is that entry's, wherever git holds it
		// now (a worktree moved by git): the entry covers it.
		if _, listed := hub.Config.Branches[wt.Branch]; listed && wt.Branch != "" {
			continue
		}
		list = append(list, unregisteredWorktree{
			Path:     wt.Path,
			Branch:   wt.Branch,
			Detached: wt.Detached || wt.Branch == "",
			Missing:  !hop.WorktreeDirPresent(fs, wt.Path),
		})
	}
	return list
}

func samePathAny(path string, paths []string) bool {
	for _, p := range paths {
		if state.SamePath(path, p) {
			return true
		}
	}
	return false
}

// findUnregisteredWorktree is the unregistered worktree target names: by
// the branch checked out in it, or by its path (absolute, or relative to
// cwd or to the hub).
func findUnregisteredWorktree(fs afero.Fs, g git.GitInterface, hub *hop.Hub, hubPath, cwd, target string) (unregisteredWorktree, bool) {
	list := unregisteredWorktrees(fs, g, hub, hubPath)
	for _, wt := range list {
		if wt.Branch != "" && wt.Branch == target {
			return wt, true
		}
	}
	for _, base := range []string{cwd, hubPath} {
		p := target
		if !filepath.IsAbs(p) {
			p = filepath.Join(base, p)
		}
		for _, wt := range list {
			if state.SamePath(wt.Path, p) {
				return wt, true
			}
		}
	}
	return unregisteredWorktree{}, false
}

// key is the name a removal knows the worktree by: its branch, else its
// path.
func (w unregisteredWorktree) key() string {
	if w.Branch != "" {
		return w.Branch
	}
	return w.Path
}

// adoptTransiently lets the removal pipeline, which reads a worktree's
// path from hop.json's entry, see an unregistered worktree as one. The
// entry lives in memory only: nothing is written to hop.json, and a
// removal that the gate refuses leaves hop.json as it was. The removal's
// own hop.json step finds nothing to drop and clears the entry.
func adoptTransiently(hub *hop.Hub, key, path string) {
	if _, ok := hub.Config.Branches[key]; ok {
		return
	}
	hub.Config.Branches[key] = config.HubBranch{Path: path}
}

// reportKeptBranch tells, in the human output, why a removed worktree's
// local branch stays. A branch that stays because the probe or the
// deletion failed is the removal's closing warning instead.
func reportKeptBranch(o branchOutcome) {
	if o.KeptReason == "" || o.Failed {
		return
	}
	if o.Branch == "" {
		output.Info("No local branch deleted: %s", o.KeptReason)
		return
	}
	output.Info("Local branch %s not deleted: %s", o.Branch, o.KeptReason)
}

// previewBranchOutcome is the dry-run's line for what removal does with
// the local branch outcome names, and, when asked, with origin's. The
// remote is never probed: previewing must not wait on the network.
func previewBranchOutcome(o branchOutcome, remote bool) {
	if o.Delete {
		output.Info("[dry-run] Would delete local branch '%s'", o.Branch)
	} else if o.KeptReason != "" {
		output.Info("[dry-run] Would keep local branch '%s': %s", o.Branch, o.KeptReason)
	}
	if remote {
		output.Info("[dry-run] Would delete branch '%s' on origin, if it exists", o.Branch)
	}
}
