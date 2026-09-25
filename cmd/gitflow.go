package cmd

import (
	"fmt"
	"strings"
	"sync"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/internal/detector"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
)

// newBranchDetectors builds the branch-type detector chain add and remove
// run: git-flow-next first, then the generic prefix table. Detection only
// reads git config and always runs, so hooks get GIT_HOP_BRANCH_* either
// way; git-flow's start/finish run only when hop.gitflow.enabled is true.
func newBranchDetectors(fs afero.Fs, g git.GitInterface, repoPath string) *detector.Manager {
	return branchDetectors(fs, g, newGitflowDetector(g, repoPath))
}

// branchDetectors is newBranchDetectors around a given git-flow detector.
func branchDetectors(fs afero.Fs, g git.GitInterface, gitflow *detector.GitFlowNextDetector) *detector.Manager {
	mgr := detector.NewManager(fs, g)
	mgr.Register(gitflow)
	mgr.Register(detector.NewGenericDetector(detector.DefaultGenericConfig()))
	return mgr
}

// newGitflowDetector returns the git-flow-next detector with its actions
// gated on hop.gitflow.enabled for the repository at repoPath, the same
// repository its gitflow.* config is read from.
func newGitflowDetector(g git.GitInterface, repoPath string, opts ...detector.GitFlowOption) *detector.GitFlowNextDetector {
	return detector.NewGitFlowNextDetector(g, append([]detector.GitFlowOption{
		detector.WithGitFlowActions(gitflowEnabled(repoPath)),
		detector.WithSkippedAction(func(*detector.BranchTypeInfo, string) { hintGitflowOptIn() }),
	}, opts...)...)
}

// gitflowStartBase maps add's --from onto git flow start's [base]: empty
// (no --from) leaves the start-point to the branch type, the
// "default-branch" sentinel names the default branch, and "initial" the
// root commit. Anything else is a ref and passes through.
func gitflowStartBase(g git.GitInterface, hubPath, from, defaultBranch string) string {
	switch from {
	case hop.StartPointDefaultBranch:
		return defaultBranch
	case hop.StartPointInitial:
		out, err := g.RunInDir(hubPath, "git", "rev-list", "--max-parents=0", "HEAD")
		if err != nil {
			return from
		}
		lines := strings.Fields(out)
		if len(lines) == 0 {
			return from
		}
		return lines[len(lines)-1]
	}
	return from
}

// gitflowStartsNewBranch reports whether add hands creating branch to git
// flow start: the git-flow detector would start it and it exists neither
// locally nor on origin. An existing branch is checked out as usual, as
// git flow start would refuse it.
func gitflowStartsNewBranch(g git.GitInterface, gitflow *detector.GitFlowNextDetector, info *detector.BranchTypeInfo, hubPath, branch string) bool {
	return gitflow.StartsBranch(info) &&
		!refExists(g, hubPath, "refs/heads/"+branch) &&
		!refExists(g, hubPath, "refs/remotes/origin/"+branch)
}

// checkGitflowStarted fails when git flow start left the worktree at path
// on anything but branch, for a git-flow that did not check it out there.
func checkGitflowStarted(g git.GitInterface, path, branch string) error {
	cur, err := g.GetCurrentBranch(path)
	if err != nil {
		return fmt.Errorf("git flow start: cannot read the branch of %s: %w", path, err)
	}
	if cur != branch {
		return fmt.Errorf("git flow start left %s on '%s', not '%s'", path, cur, branch)
	}
	return nil
}

// undoGitflowWorktree removes the detached worktree add created for git
// flow start to create branch in, after the start failed, along with the
// branch if the start got as far as creating it.
func undoGitflowWorktree(fs afero.Fs, g git.GitInterface, hubPath, worktreePath, branch string) {
	if err := g.WorktreeRemove(hubPath, worktreePath, true); err != nil {
		output.Warn("Failed to remove worktree via git: %v", err)
	}
	if err := fs.RemoveAll(worktreePath); err != nil {
		output.Warn("Failed to remove worktree directory: %v", err)
	}
	if err := hop.NewCleanupManager(fs, g).RemoveEmptyParent(worktreePath, hubPath); err != nil {
		output.Warn("Failed to remove empty parent directory: %v", err)
	}
	if refExists(g, hubPath, "refs/heads/"+branch) {
		if err := g.DeleteLocalBranch(hubPath, branch); err != nil {
			output.Warn("Failed to delete local branch: %v", err)
		}
	}
}

// gitflowFinishes reports whether removing branch from the hub at hubPath
// runs git flow finish for it, detected as removeBranchWorktree detects
// it. A detection error reports false: the removal itself fails on it.
func gitflowFinishes(fs afero.Fs, g git.GitInterface, hubPath, branch string) bool {
	gitflow := newGitflowDetector(g, hubPath)
	info, err := branchDetectors(fs, g, gitflow).DetectBranch(branch, hubPath)
	return err == nil && gitflow.FinishesBranch(info)
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
		output.Hint("git-flow is set up but git hop runs no 'git flow' commands; to run start/finish on add/remove: git config %s true",
			config.KeyGitflowEnabled)
	})
}
