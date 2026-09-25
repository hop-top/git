package cmd

import (
	"path/filepath"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
)

// registerConvertedHub records a just-converted repository in git-hop's
// registry and state, as clone does for a new hub, so list, status --all
// and prune see it without a `git hop add` first. The initial worktree is
// hops/<branch> for a bare conversion and the repository root for a
// regular one. The linked worktrees a bare conversion carried are
// recorded too, as registerAdoptedHub records a hub's existing ones:
// quietly, since the conversion did not create them. Never called on a
// dry run, which returns before converting. It reports whether state now
// records the hub.
func registerConvertedHub(fs afero.Fs, hub *hop.Hub, repoPath, worktreePath, branch string, isRegular bool) bool {
	if hub == nil || worktreePath == "" {
		return false
	}
	wtType := hop.WorktreeTypeBare
	if isRegular {
		wtType = hop.WorktreeTypeMain
	}
	linked := map[string]string{}
	for b := range hub.Config.Branches {
		if b == branch {
			continue
		}
		if path := hub.BranchPath(b); hop.WorktreeDirPresent(fs, path) {
			linked[b] = path
		}
	}
	_, err := hop.RegisterNewHub(fs, hop.NewHub{
		URI:           hub.Config.Repo.URI,
		Org:           hub.Config.Repo.Org,
		Repo:          hub.Config.Repo.Repo,
		DefaultBranch: branch,
		HubPath:       repoPath,
		WorktreePath:  worktreePath,
		WorktreeType:  wtType,
		Linked:        linked,
	})
	return err == nil
}

// registerAsIsHub records a repository registered as-is (init menu option
// 3) the way registerConvertedHub records a converted one, so list,
// status --all and prune see it. Its structure is unchanged: the hub is
// the repository root, which is also its branch's worktree, and no
// hop.json is written.
func registerAsIsHub(fs afero.Fs, uri, org, repo, branch, repoPath string) {
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		absPath = repoPath
	}
	hop.RegisterNewHub(fs, hop.NewHub{
		URI:           uri,
		Org:           org,
		Repo:          repo,
		DefaultBranch: branch,
		HubPath:       absPath,
		WorktreePath:  absPath,
		WorktreeType:  hop.WorktreeTypeMain,
	})
}

// registerAdoptedHub records a hub whose hop.json init just back-filled,
// i.e. one git-hop did not create (a bare clone made with plain git), the
// way clone and conversion record theirs. It reports whether state now
// records the hub.
func registerAdoptedHub(fs afero.Fs, hubPath string) bool {
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		output.Warn("failed to read hop.json at %s: %v", hubPath, err)
		return false
	}
	_, err = hop.RegisterNewHub(fs, hubFromConfig(fs, hub))
	return err == nil
}

// hubFromConfig describes the hub at hub.Path, as its hop.json lists it,
// for hop.RegisterNewHub: every branch whose worktree directory is there,
// the default branch's as the hub's initial worktree and the rest as
// linked ones. A row whose directory is gone is left out; state would
// only record a missing worktree.
func hubFromConfig(fs afero.Fs, hub *hop.Hub) hop.NewHub {
	absHub, err := filepath.Abs(hub.Path)
	if err != nil {
		absHub = hub.Path
	}
	repo := hub.Config.Repo
	h := hop.NewHub{
		URI:           repo.URI,
		Org:           repo.Org,
		Repo:          repo.Repo,
		DefaultBranch: repo.DefaultBranch,
		HubPath:       absHub,
		Linked:        map[string]string{},
		Global:        repo.Mode == config.RepoModeGlobal,
	}
	for branch := range hub.Config.Branches {
		path := hub.BranchPath(branch)
		if !hop.WorktreeDirPresent(fs, path) {
			continue
		}
		if branch != repo.DefaultBranch {
			h.Linked[branch] = path
			continue
		}
		h.WorktreePath = path
		h.WorktreeType = hop.WorktreeTypeBare
		if filepath.Clean(path) == absHub {
			h.WorktreeType = hop.WorktreeTypeMain
		}
	}
	return h
}

// restoreAdoptedFetchRefspec gives a hub init just adopted the origin
// fetch refspec that a plain `git clone --bare` leaves out, as clone
// does for its hubs; without it origin/* never moves on fetch.
func restoreAdoptedFetchRefspec(g git.GitInterface, hubPath string) {
	restored, err := hop.RestoreOriginFetchRefspec(g, hubPath)
	switch {
	case err != nil:
		output.Warn("failed to set remote.origin.fetch at %s: %v", hubPath, err)
	case restored:
		output.Note("Set remote.origin.fetch to %s.", hop.OriginFetchRefspec)
	}
}
