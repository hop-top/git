package cmd

import (
	"fmt"
	"os"
	"sync"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/internal/detector"
	"hop.top/git/internal/git"
	"hop.top/git/internal/output"
)

// newBranchDetectors builds the branch-type detector chain add and remove
// run: git-flow-next first, then the generic prefix table. Detection only
// reads git config and always runs, so hooks get GIT_HOP_BRANCH_* either
// way; git-flow's start/finish run only when hop.gitflow.enabled is true.
func newBranchDetectors(fs afero.Fs, g git.GitInterface, repoPath string) *detector.Manager {
	mgr := detector.NewManager(fs, g)
	mgr.Register(newGitflowDetector(g, repoPath))
	mgr.Register(detector.NewGenericDetector(detector.DefaultGenericConfig()))
	return mgr
}

// newGitflowDetector returns the git-flow-next detector with its actions
// gated on hop.gitflow.enabled for the repository at repoPath, the same
// repository its gitflow.* config is read from.
func newGitflowDetector(g git.GitInterface, repoPath string) *detector.GitFlowNextDetector {
	return detector.NewGitFlowNextDetector(g,
		detector.WithGitFlowActions(gitflowEnabled(repoPath)),
		detector.WithSkippedAction(func(*detector.BranchTypeInfo, string) { hintGitflowOptIn() }),
	)
}

func gitflowEnabled(repoPath string) bool {
	return config.NewGitConfigIn(repoPath).GetBoolOrDefault(config.KeyGitflowEnabled)
}

var gitflowHintOnce sync.Once

// hintGitflowOptIn tells a --verbose user, at most once per command, that
// a git-flow action was skipped and which setting turns it on. Verbose
// only: skipping is the intended default, not something to fix.
func hintGitflowOptIn() {
	if !output.IsVerbose() || !output.IsModeHuman() {
		return
	}
	gitflowHintOnce.Do(func() {
		fmt.Fprintf(os.Stderr, "hint: git-flow is set up but git hop runs no 'git flow' commands; to run start/finish on add/remove: git config %s true\n",
			config.KeyGitflowEnabled)
	})
}
