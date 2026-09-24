package hop

import (
	"path/filepath"
	"strings"

	"hop.top/git/internal/git"
)

// A sparse checkout set up before `git sparse-checkout` existed, or by
// hand, keeps core.sparseCheckout in the shared .git/config, with no
// extensions.worktreeConfig. The repository has one worktree, so shared
// and per-worktree are the same thing there. In a hub they are not: set
// in the hub's shared config, core.sparseCheckout would apply to every
// worktree `git hop add` creates, each a full checkout git then reports
// as "not sparse". git-worktree(1) (CONFIGURATION FILE) recommends
// against sharing it. So a bare conversion treats these keys as the
// default worktree's own, as `git sparse-checkout` itself does: they go
// to its config.worktree, the hub gets the extension with core.bare in
// its own config.worktree (carryOverWorktreeConfig), and the sparse
// checkout is applied again there.

// sparseWorktreeKeys are the keys that describe one worktree's sparse
// checkout; `git sparse-checkout` writes them with --worktree.
var sparseWorktreeKeys = []string{"core.sparsecheckout", "core.sparsecheckoutcone", "index.sparse"}

func isSparseWorktreeKey(key string) bool {
	for _, k := range sparseWorktreeKeys {
		if strings.EqualFold(key, k) {
			return true
		}
	}
	return false
}

// planSparseToWorktree moves the sparse keys of a repository without
// extensions.worktreeConfig from plan.Carried to plan.PerWorktree, when
// its own config file turns the sparse checkout on. Otherwise the plan is
// left as it is: sparse keys that switch nothing on are inert anywhere.
func planSparseToWorktree(g git.GitInterface, repoPath string, plan *LocalConfigPlan) {
	file := filepath.Join(repoPath, ".git", "config")
	on, err := g.Run("git", "config", "--file", file, "--no-includes", "--type=bool", "--get", "core.sparseCheckout")
	if err != nil || strings.TrimSpace(on) != "true" {
		// Unset, or not a boolean git would accept: nothing to move.
		return
	}
	var carried []configEntry
	for _, e := range plan.Carried {
		if isSparseWorktreeKey(e.key) {
			plan.PerWorktree = append(plan.PerWorktree, e)
			continue
		}
		carried = append(carried, e)
	}
	plan.Carried = carried
	plan.SparseToWorktree = true
}
