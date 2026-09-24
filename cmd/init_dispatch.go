package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/hop"
)

// initRepoID resolves the 3-part repo ID ("host/org/repo") init uses for
// the hopspace: org/repo from the remote URL, falling back to the local
// path (matches registerAsIs). Returns "" when neither yields a name.
func initRepoID(g git.GitInterface, repoPath string) string {
	org, repo := "", ""
	if g != nil {
		if remoteURL, err := g.GetRemoteURL(repoPath); err == nil && remoteURL != "" {
			org, repo = hop.ParseRepoFromURL(remoteURL)
		}
	}
	if org == "" || repo == "" {
		abs, err := filepath.Abs(repoPath)
		if err == nil {
			repo = filepath.Base(abs)
			org = filepath.Base(filepath.Dir(abs))
		}
	}
	if org == "" || repo == "" {
		return ""
	}
	return fmt.Sprintf("github.com/%s/%s", org, repo)
}

// dispatchInitWorktreeAdd fires post-worktree-add for the initial worktree
// init created, through the same dispatch clone uses. A failing hook warns
// and does not undo the conversion, matching clone.
func dispatchInitWorktreeAdd(fs afero.Fs, g git.GitInterface, repoPath, worktreePath, branch string) {
	repoID := initRepoID(g, repoPath)
	if err := cli.BuildHookDispatch(fs).PostWorktreeAdd(worktreePath, repoID, branch); err != nil {
		fmt.Fprintf(os.Stderr, "warning: post-worktree-add hook failed: %v\n", err)
	}
}

// previewInitWorktreeAdd reports the post-worktree-add hook a bare
// conversion would dispatch for hops/<branch>, without running it.
func previewInitWorktreeAdd(fs afero.Fs, g git.GitInterface, repoPath, branch string) {
	if branch == "" {
		return
	}
	worktreePath := filepath.Join(repoPath, "hops", branch)
	cli.PreviewHook(hooks.NewRunner(fs), "post-worktree-add", worktreePath, initRepoID(g, repoPath))
}
