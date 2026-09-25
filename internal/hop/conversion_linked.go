package hop

import (
	"errors"
	"fmt"
	"strings"

	"hop.top/git/internal/git"
)

// linkedWorktrees lists the linked worktrees of the repository at repo
// that still exist on disk, as `git worktree list` reports them. The main
// worktree and entries git marks prunable are left out.
func linkedWorktrees(g git.GitInterface, repo string) ([]string, error) {
	out, err := g.WorktreeListPorcelain(repo)
	if err != nil {
		return nil, fmt.Errorf("failed to list the worktrees of %s: %w", repo, err)
	}
	var paths []string
	for i, rec := range strings.Split(strings.TrimSpace(out), "\n\n") {
		if i == 0 {
			continue // the main worktree always comes first
		}
		var path string
		prunable := false
		for _, line := range strings.Split(rec, "\n") {
			if p, ok := strings.CutPrefix(line, "worktree "); ok {
				path = p
			}
			if line == "prunable" || strings.HasPrefix(line, "prunable ") {
				prunable = true
			}
		}
		if path != "" && !prunable {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

// ErrLinkedWorktrees is the refusal of a bare conversion of a repository
// with linked worktrees; the details are in the result's errors.
var ErrLinkedWorktrees = errors.New("linked worktrees present")

// refuseLinkedWorktrees stops a bare conversion of a repository that has
// linked worktrees. Their admin dirs live in the .git directory the
// conversion replaces, so they would be left pointing at nothing, their
// index and per-worktree state lost. A regular conversion keeps .git in
// place and is not affected.
func refuseLinkedWorktrees(g git.GitInterface, repo string) error {
	linked, err := linkedWorktrees(g, repo)
	if err != nil {
		return err
	}
	if len(linked) == 0 {
		return nil
	}
	return fmt.Errorf("repository has %d linked worktree(s) (%s); a bare conversion "+
		"would disconnect them from the repository. Remove them with 'git worktree remove', "+
		"or convert with --regular, which keeps .git in place",
		len(linked), strings.Join(linked, ", "))
}
