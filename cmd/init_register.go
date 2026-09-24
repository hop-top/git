package cmd

import (
	"path/filepath"

	"github.com/spf13/afero"
	"hop.top/git/internal/hop"
)

// registerConvertedHub records a just-converted repository in git-hop's
// registry and state, as clone does for a new hub, so list, status --all
// and prune see it without a `git hop add` first. The initial worktree is
// hops/<branch> for a bare conversion and the repository root for a
// regular one. Never called on a dry run, which returns before converting.
func registerConvertedHub(fs afero.Fs, hub *hop.Hub, repoPath, worktreePath, branch string, isRegular bool) {
	if hub == nil || worktreePath == "" {
		return
	}
	wtType := hop.WorktreeTypeBare
	if isRegular {
		wtType = hop.WorktreeTypeMain
	}
	hop.RegisterNewHub(fs, hop.NewHub{
		URI:           hub.Config.Repo.URI,
		Org:           hub.Config.Repo.Org,
		Repo:          hub.Config.Repo.Repo,
		DefaultBranch: branch,
		HubPath:       repoPath,
		WorktreePath:  worktreePath,
		WorktreeType:  wtType,
	})
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
