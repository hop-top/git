package cmd

import (
	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
)

// isHubStructure reports whether s is a git-hop hub's root: a bare
// repository, or a regular one holding hop.json.
func isHubStructure(s config.StructureType) bool {
	return s == config.BareWorktreeRoot || s == config.WorktreeRoot
}

// repoOfLinkedWorktree returns the root of the repository the linked
// worktree at dir belongs to, from git's common dir, and that root's
// structure. ok is false when git does not report dir as a linked
// worktree.
func repoOfLinkedWorktree(fs afero.Fs, g git.GitInterface, dir string) (string, config.StructureType, bool) {
	root, ok := hop.RepoRootOfWorktree(g, dir)
	if !ok {
		return "", config.UnknownStructure, false
	}
	return root, hop.DetectRepoStructure(fs, g, root), true
}

// resolveInitTarget decides which repository init acts on. Run from a
// linked worktree of a repository that is not a hub yet, init acts on
// that repository, as git commands run from a linked worktree do: it is
// offered the conversion (a bare one refused while linked worktrees
// exist), instead of being reported as already initialized. A linked
// worktree of a hub, and anything else, is left as detected.
func resolveInitTarget(fs afero.Fs, g git.GitInterface, cwd string, s config.StructureType) (string, config.StructureType) {
	if s != config.WorktreeChild {
		return cwd, s
	}
	root, rs, ok := repoOfLinkedWorktree(fs, g, cwd)
	if !ok || isHubStructure(rs) {
		return cwd, s
	}
	output.Note("%s is a linked worktree of %s; init acts on that repository", cwd, root)
	return root, rs
}
