package cmd

import (
	"errors"
	"fmt"
	"strings"

	"hop.top/git/internal/git"
	"hop.top/git/internal/output"
)

// mergePlan is everything `git hop merge` has decided before its first write.
type mergePlan struct {
	source, into         string
	sourcePath, intoPath string
	noFF, deleteRemote   bool
}

// previewMerge reports what `git hop merge` would do for p without doing
// any of it: the receiving branch does not move, and the source worktree,
// branch, hop.json, hopspace, state and symlink are left alone. A merge
// that would stop on conflicts fails here too.
func previewMerge(g git.GitInterface, p mergePlan) {
	mode, err := mergeMode(g, p)
	if err != nil {
		refuseDryRun(fmt.Sprintf("merge '%s' into '%s'", p.source, p.into), err)
	}

	output.Info("[dry-run] Would merge '%s' into '%s' (%s)", p.source, p.into, mode)
	output.Info("[dry-run] Would remove worktree at %s", p.sourcePath)
	previewBranchDeletion(p.source, true, p.deleteRemote)
	output.Info("[dry-run] Would remove '%s' from hop.json, hopspace and state", p.source)
	output.Info("[dry-run] Would point 'current' at '%s'", p.into)
}

// mergeMode names the merge `git merge` would perform, or returns an error
// when it would stop on conflicts.
//
// The conflict probe is `git merge-tree --write-tree`, which merges in
// memory: no ref, index or worktree changes. Like the content-equivalence
// probe in inspectBranchSafety, it may leave unreachable objects behind for
// gc. When the probe cannot run at all (git older than 2.38), no conflict
// verdict is claimed.
func mergeMode(g git.GitInterface, p mergePlan) (string, error) {
	isAncestor := func(a, b string) bool {
		_, err := g.RunInDir(p.intoPath, "git", "merge-base", "--is-ancestor", a, b)
		return err == nil
	}

	switch {
	case isAncestor(p.source, p.into):
		return "already up to date", nil
	case !p.noFF && isAncestor(p.into, p.source):
		return "fast-forward", nil
	}

	out, err := g.RunInDir(p.intoPath, "git", "merge-tree", "--write-tree", p.into, p.source)
	if err != nil && strings.Contains(out, "CONFLICT") {
		return "", errors.New("merge would stop on conflicts")
	}
	return "merge commit", nil
}
