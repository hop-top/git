package hop

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/output"
)

// AttachRefusal is why ForkAttach would not reuse a worktree already at
// the path an attach puts one: it is on another branch, is not a worktree
// of the repository, holds work of its own, or has local changes the
// update would overwrite. Nothing in that worktree was changed. Hint says
// how to get past it.
type AttachRefusal struct {
	Msg  string
	Hint string
}

func (e *AttachRefusal) Error() string { return e.Msg }

// listedWorktree is one worktree as `git worktree list --porcelain`
// records it.
type listedWorktree struct {
	Path     string
	Head     string
	Branch   string // short name; empty when detached
	Detached bool
}

// worktreeListedAt returns the worktree of the repository at repoDir
// whose path is path, if git lists one.
func worktreeListedAt(g git.GitInterface, repoDir, path string) (listedWorktree, bool, error) {
	out, err := g.WorktreeListPorcelain(repoDir)
	if err != nil {
		return listedWorktree{}, false, fmt.Errorf("failed to list worktrees of %s: %v", repoDir, err)
	}
	for _, record := range strings.Split(strings.TrimSpace(out), "\n\n") {
		var wt listedWorktree
		for _, line := range strings.Split(record, "\n") {
			switch {
			case strings.HasPrefix(line, "worktree "):
				wt.Path = strings.TrimPrefix(line, "worktree ")
			case strings.HasPrefix(line, "HEAD "):
				wt.Head = strings.TrimPrefix(line, "HEAD ")
			case strings.HasPrefix(line, "branch "):
				wt.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
			case line == "detached":
				wt.Detached = true
			}
		}
		if wt.Path != "" && samePath(wt.Path, path) {
			return wt, true, nil
		}
	}
	return listedWorktree{}, false, nil
}

// existingForkWorktree returns where the fork's hopspace already has a
// worktree for branch, or "" when it has none: the path its hop.json
// records for the branch, else the path an attach creates it at.
func existingForkWorktree(fs afero.Fs, hopspace *Hopspace, hopspacePath, branch string) string {
	var candidates []string
	if b, ok := hopspace.Config.Branches[branch]; ok && b.Path != "" {
		candidates = append(candidates, config.ResolveWorktreePath(b.Path, hopspacePath))
	}
	candidates = append(candidates, filepath.Join(hopspacePath, "hops", branch))
	for _, p := range candidates {
		if ok, _ := afero.Exists(fs, p); ok {
			return p
		}
	}
	return ""
}

// reuseForkWorktree brings the fork hopspace's worktree at path, already
// on branch, to the fork's branch as fetched into origin/<branch>. It
// moves the branch only when every commit on it is on a remote: a fast
// forward, or a branch left behind by an attach that started it from the
// fork's first branch. Local changes are carried over unless the update
// would overwrite them, in which case git refuses and so does this.
func reuseForkWorktree(g git.GitInterface, repoDir, path, branch string) error {
	wt, listed, err := worktreeListedAt(g, repoDir, path)
	if err != nil {
		return err
	}
	if !listed {
		return &AttachRefusal{
			Msg:  fmt.Sprintf("%s exists but is not a worktree of the fork's repository", path),
			Hint: "move it out of the way, then attach again",
		}
	}
	if wt.Branch != branch {
		return &AttachRefusal{
			Msg:  fmt.Sprintf("the fork's worktree at %s is %s, not %s", path, describeHead(wt), branch),
			Hint: fmt.Sprintf("switch it back to %s (git -C %s switch %s), then attach again", branch, path, branch),
		}
	}
	own, err := g.RunInDir(path, "git", "rev-list", "refs/heads/"+branch, "--not", "--remotes")
	if err != nil {
		return fmt.Errorf("failed to compare %s with the fork: %v", branch, err)
	}
	if strings.TrimSpace(own) != "" {
		return &AttachRefusal{
			Msg:  fmt.Sprintf("branch %s in the fork's worktree at %s has commits on no branch of the fork (unpushed, or the fork rewrote %s)", branch, path, branch),
			Hint: fmt.Sprintf("push them, or keep them on another branch and reset %s to origin/%s there, then attach again", branch, branch),
		}
	}
	output.Info("Reusing the fork's worktree at %s", path)
	if _, err := g.RunInDir(path, "git", "checkout", "-B", branch, "refs/remotes/origin/"+branch); err != nil {
		return &AttachRefusal{
			Msg:  fmt.Sprintf("cannot update the fork's worktree at %s: %v", path, err),
			Hint: fmt.Sprintf("commit or stash the changes in %s, then attach again", path),
		}
	}
	return nil
}

// checkHubForkWorktree reports what is already at path, where the hub's
// worktree of the fork's branch goes, before anything is changed: nil
// when nothing is, else the worktree to reuse. Only a worktree of the
// hub's repository with a detached HEAD, as an attach leaves it, is
// reused; anything else is refused.
func checkHubForkWorktree(fs afero.Fs, g git.GitInterface, mainRepoPath, path string) (*listedWorktree, error) {
	if ok, _ := afero.Exists(fs, path); !ok {
		return nil, nil
	}
	wt, listed, err := worktreeListedAt(g, mainRepoPath, path)
	if err != nil {
		return nil, err
	}
	if !listed {
		return nil, &AttachRefusal{
			Msg:  fmt.Sprintf("%s exists but is not a worktree of this hub", path),
			Hint: "move it out of the way, then attach again",
		}
	}
	if !wt.Detached {
		return nil, &AttachRefusal{
			Msg:  fmt.Sprintf("the fork's worktree at %s is %s; an attach leaves it detached at the fork's commit", path, describeHead(wt)),
			Hint: fmt.Sprintf("push or move that branch's work, detach it (git -C %s switch --detach), then attach again", path),
		}
	}
	return &wt, nil
}

// updateHubForkWorktree moves the detached worktree wt to commit when
// that is a fast forward. Any other move would leave commits made there
// behind, so it is refused, as is one that would overwrite local changes.
// hubBranch is the hub branch the worktree is registered as.
func updateHubForkWorktree(g git.GitInterface, wt *listedWorktree, commit, hubBranch string) error {
	output.Info("Reusing the fork's worktree at %s", wt.Path)
	if wt.Head == commit {
		return nil
	}
	base, err := g.MergeBase(wt.Path, wt.Head, commit)
	if err != nil || strings.TrimSpace(base) != wt.Head {
		return &AttachRefusal{
			Msg:  fmt.Sprintf("the fork's worktree at %s is at %s, which the fork's branch does not contain", wt.Path, shortSHA(wt.Head)),
			Hint: fmt.Sprintf("keep any work there on a branch, remove the worktree (git hop remove %s), then attach again", hubBranch),
		}
	}
	if _, err := g.RunInDir(wt.Path, "git", "checkout", "--detach", commit); err != nil {
		return &AttachRefusal{
			Msg:  fmt.Sprintf("cannot update the fork's worktree at %s: %v", wt.Path, err),
			Hint: fmt.Sprintf("commit or stash the changes in %s, then attach again", wt.Path),
		}
	}
	return nil
}

func describeHead(wt listedWorktree) string {
	if wt.Branch == "" {
		return "detached at " + shortSHA(wt.Head)
	}
	return "on branch " + wt.Branch
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
