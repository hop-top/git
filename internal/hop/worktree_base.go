package hop

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
)

// worktreeBase is where a new worktree's git commands run: base for
// reading refs and resolving the start-point (a worktree, so HEAD and
// @{-1} mean that worktree's), addDir for `git worktree add` itself.
type worktreeBase struct {
	base   string
	addDir string
}

// findBase picks both directories for a worktree added to the hub at
// hubPath. Both belong to the hub's own repository: when the hub is
// the root of a repository, base is one of that repository's
// worktrees (the repository itself when it has none) and addDir is the
// repository. The hopspace is never consulted then. Hubs sharing a
// --global hopspace are separate repositories, and the hopspace
// records every hub's worktrees, so a worktree found there can belong
// to another repository.
//
// A hub path that is not a repository root (a fork's hopspace, whose
// repositories are its worktrees) falls back to a worktree the
// hopspace records under hubPath, else hubPath itself.
func (m *WorktreeManager) findBase(hopspace *Hopspace, hubPath string) worktreeBase {
	if repo, ok := hubRepository(m.git, hubPath); ok {
		return worktreeBase{base: repoWorktree(m.fs, m.git, repo), addDir: repo}
	}
	base := hopspaceWorktreeIn(m.fs, hopspace, hubPath)
	return worktreeBase{base: base, addDir: worktreeAddDir(m.fs, m.git, base)}
}

// findBaseWorktree is findBase's base.
func (m *WorktreeManager) findBaseWorktree(hopspace *Hopspace, hubPath string) string {
	return m.findBase(hopspace, hubPath).base
}

// hubRepository returns where git runs for the repository whose root is
// hubPath: the git dir of a bare repository at hubPath or kept in
// hubPath/.git, or hubPath itself for the main worktree of a non-bare
// one (a --regular hub). ok is false when hubPath is not a repository
// root: nothing git finds, a linked worktree, or a directory inside
// some other repository.
func hubRepository(g git.GitInterface, hubPath string) (string, bool) {
	p, ok := probeRepo(g, hubPath)
	if !ok || p.linked() {
		return "", false
	}
	dotGit := filepath.Join(hubPath, ".git")
	if p.bare {
		if samePath(p.gitDir, hubPath) || samePath(p.gitDir, dotGit) {
			return p.gitDir, true
		}
		return "", false
	}
	if samePath(p.commonDir, dotGit) {
		return hubPath, true
	}
	return "", false
}

// repoWorktree returns the first worktree of the repository at repo
// that git lists and that is on disk, the repository itself when there
// is none or git cannot list them.
func repoWorktree(fs afero.Fs, g git.GitInterface, repo string) string {
	out, err := g.WorktreeListPorcelain(repo)
	if err != nil {
		return repo
	}
	for _, record := range strings.Split(strings.TrimSpace(out), "\n\n") {
		var path string
		skip := false
		for _, line := range strings.Split(record, "\n") {
			if p, ok := strings.CutPrefix(line, "worktree "); ok {
				path = strings.TrimSpace(p)
			}
			if line == "bare" || line == "prunable" || strings.HasPrefix(line, "prunable ") {
				skip = true
			}
		}
		if path == "" || skip {
			continue
		}
		if ok, _ := afero.DirExists(fs, path); ok {
			return path
		}
	}
	return repo
}

// hopspaceWorktreeIn returns a worktree the hopspace records at or
// below hubPath, hubPath itself when it records none there. One on disk
// wins over one that is not (moved away, removed with git, never
// finished), which no git command can run in; with none on disk it is
// the first recorded. Candidates go in a fixed order, the default
// branch first, then by name, so the pick never depends on how a walk
// of the branch map falls.
func hopspaceWorktreeIn(fs afero.Fs, hopspace *Hopspace, hubPath string) string {
	if hopspace == nil || hopspace.Config == nil {
		return hubPath
	}
	names := make([]string, 0, len(hopspace.Config.Branches))
	for name := range hopspace.Config.Branches {
		names = append(names, name)
	}
	sort.Strings(names)
	if def := hopspace.Config.Repo.DefaultBranch; def != "" {
		names = append([]string{def}, names...)
	}
	var recorded []string
	for _, name := range names {
		b, ok := hopspace.Config.Branches[name]
		if !ok || !b.Exists || b.Path == "" {
			continue
		}
		p := config.ResolveWorktreePath(b.Path, hubPath)
		if samePath(p, hubPath) || isStrictlyUnder(p, hubPath) {
			recorded = append(recorded, p)
		}
	}
	for _, p := range recorded {
		if WorktreeAt(fs, p) == WorktreePresent {
			return p
		}
	}
	if len(recorded) > 0 {
		return recorded[0]
	}
	return hubPath
}
