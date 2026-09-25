package cmd

import (
	"strings"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// checkWorktreeRepositories reports each hub worktree another repository
// holds: git registered it in that repository, not the hub's. Earlier
// releases could do this to hubs sharing a --global hopspace, running
// `git worktree add` in whichever hub the hopspace named. Such a
// worktree's commits and branch live in the other repository, so the
// hub's own git commands (status, merge, remove's merge check) do not
// see them.
//
// --fix has no repair: moving a worktree between repositories needs its
// work committed first, which only the user can do. The hint says how.
// A worktree git cannot answer for (missing, not a worktree root) is
// left to the other checks.
func checkWorktreeRepositories(fs afero.Fs, g git.GitInterface, hub *hop.Hub, r *doctorReport) {
	own, ok := repositoryAt(g, hub.Path)
	if !ok {
		return
	}
	for _, name := range sortedBranchNames(hub) {
		path := config.ResolveWorktreePath(hub.Config.Branches[name].Path, hub.Path)
		if hop.WorktreeAt(fs, path) != hop.WorktreePresent {
			continue
		}
		holder, ok := worktreeRepository(g, path)
		if !ok || state.SamePath(holder, own) {
			continue
		}
		output.Error("Worktree of branch %s at %s is registered in %s, not in this hub's repository", name, path, holder)
		hint := foreignWorktreeHint(hub.Path, holder, name, path)
		output.Hint("%s", hint)
		r.unfixableIssue(doctorCheckHub, name, "worktree %s is registered in %s, not in this hub's repository %s; %s",
			path, holder, own, hint)
	}
}

// repositoryAt returns the absolute common dir of the repository git
// finds from dir.
func repositoryAt(g git.GitInterface, dir string) (string, bool) {
	out, err := g.RunInDir(dir, "git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	common := strings.TrimSpace(out)
	if err != nil || common == "" || strings.Contains(common, "\n") {
		return "", false
	}
	return common, true
}

// foreignWorktreeHint says how to move the worktree of branch at path
// from repository holder into the hub: bring the branch over, drop the
// other repository's worktree, and let doctor --fix check it out again
// in the hub.
func foreignWorktreeHint(hubPath, holder, branch, path string) string {
	return "commit its work, then fetch the branch into this hub (git -C " + hubPath + " fetch " + holder + " " +
		branch + ":" + branch + "), remove the worktree there (git -C " + holder + " worktree remove " + path +
		") and run 'git hop doctor --fix' to recreate it here"
}
