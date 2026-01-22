package hop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/git"
	"github.com/spf13/afero"
)

func DetectRepoStructure(fs afero.Fs, path string) config.StructureType {
	gitDir := filepath.Join(path, ".git")
	gitInfo, err := fs.Stat(gitDir)
	if err != nil {
		if os.IsNotExist(err) {
			return config.NotGit
		}
		return config.UnknownStructure
	}

	worktreesDir := filepath.Join(gitDir, "worktrees")
	_, err = fs.Stat(worktreesDir)
	if err == nil {
		if IsWorktree(fs, path) {
			return config.WorktreeChild
		}

		headPath := filepath.Join(gitDir, "HEAD")
		headInfo, err := os.Stat(headPath)
		if err != nil {
			return config.UnknownStructure
		}

		if headInfo.Mode()&os.ModeSymlink != 0 || headInfo.Mode().IsRegular() {
			return config.BareWorktreeRoot
		}

		return config.WorktreeRoot
	}

	if gitInfo.IsDir() {
		return config.StandardRepo
	}

	if gitInfo.Mode()&os.ModeSymlink != 0 {
		return config.WorktreeChild
	}

	content, err := afero.ReadFile(fs, gitDir)
	if err == nil {
		if strings.Contains(string(content), "gitdir:") {
			return config.WorktreeChild
		}
	}

	return config.StandardRepo
}

func IsWorktree(fs afero.Fs, path string) bool {
	gitFile := filepath.Join(path, ".git")
	info, err := os.Stat(gitFile)
	if err != nil {
		return false
	}

	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}

	if info.Mode().IsRegular() {
		content, err := afero.ReadFile(fs, gitFile)
		if err == nil {
			return strings.Contains(string(content), "gitdir:")
		}
	}

	return false
}

func FindProjectRoot(fs afero.Fs, path string) (string, error) {
	currentPath := path
	for {
		structure := DetectRepoStructure(fs, currentPath)

		if structure == config.BareWorktreeRoot || structure == config.WorktreeRoot {
			return currentPath, nil
		}

		if structure == config.NotGit {
			break
		}

		parent := filepath.Dir(currentPath)
		if parent == currentPath {
			break
		}

		currentPath = parent
	}

	return "", fmt.Errorf("project root not found")
}

func GetCurrentWorktreeBranch(g *git.Git, path string) (string, error) {
	if !g.IsInsideWorkTree(path) {
		return "", fmt.Errorf("not inside a git worktree")
	}

	branch, err := g.GetCurrentBranch(path)
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}

	return branch, nil
}

func GetRepoRoot(g *git.Git, path string) (string, error) {
	if !g.IsInsideWorkTree(path) {
		return "", fmt.Errorf("not inside a git repository")
	}

	root, err := g.GetRoot(path)
	if err != nil {
		return "", fmt.Errorf("failed to get repo root: %w", err)
	}

	return root, nil
}

func ListWorktrees(g *git.Git, repoRoot string) ([]string, error) {
	out, err := g.Runner.RunInDir(repoRoot, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees: %w", err)
	}

	lines := strings.Split(out, "\n")
	var worktrees []string
	var currentWorktree string

	for _, line := range lines {
		if strings.HasPrefix(line, "worktree ") {
			currentWorktree = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "HEAD ") {
			branch := strings.TrimPrefix(line, "HEAD ")
			branch = strings.TrimPrefix(branch, "refs/heads/")
			if currentWorktree != "" && branch != "" {
				worktrees = append(worktrees, branch)
			}
		}
	}

	return worktrees, nil
}

func GetWorktreePath(g *git.Git, repoRoot, branch string) (string, error) {
	out, err := g.Runner.RunInDir(repoRoot, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("failed to list worktrees: %w", err)
	}

	lines := strings.Split(out, "\n")
	var currentPath string

	for i, line := range lines {
		if strings.HasPrefix(line, "worktree ") {
			currentPath = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "HEAD ") && i > 0 {
			branchRef := strings.TrimPrefix(line, "HEAD ")
			branchRef = strings.TrimPrefix(branchRef, "refs/heads/")
			if branchRef == branch && currentPath != "" {
				return currentPath, nil
			}
		}
	}

	return "", fmt.Errorf("worktree not found for branch: %s", branch)
}
