package hop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"github.com/spf13/afero"
)

// LooksLikeGitCheckout reports whether path is any flavor of git checkout
// — a standard repo (.git directory), a worktree child (.git file with
// gitdir: pointer), or a bare repo at root (HEAD/objects/refs directly
// under path, the shape cloneBareRepo produces for hop hubs).
//
// Use this when you need a permissive "is this a place where it's safe
// to install hop metadata" check, rather than a structural classifier.
// DetectRepoStructure is the structural classifier and returns more
// specific information when callers care about WHICH flavor.
func LooksLikeGitCheckout(fs afero.Fs, path string) bool {
	if isBareRepoAtPath(fs, path) {
		return true
	}
	gitPath := filepath.Join(path, ".git")
	info, err := fs.Stat(gitPath)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return true
	}
	// .git is a file — worktree child shape ("gitdir: ..." pointer).
	if info.Mode().IsRegular() {
		content, err := afero.ReadFile(fs, gitPath)
		if err == nil && strings.Contains(string(content), "gitdir:") {
			return true
		}
	}
	return false
}

// isBareRepoAtPath reports whether path is itself a bare git repository
// (no .git subdir; HEAD/objects/refs sit directly under path). This is
// the shape produced by cloneBareRepo for hop hubs.
func isBareRepoAtPath(fs afero.Fs, path string) bool {
	headInfo, err := fs.Stat(filepath.Join(path, "HEAD"))
	if err != nil || !headInfo.Mode().IsRegular() {
		return false
	}
	objInfo, err := fs.Stat(filepath.Join(path, "objects"))
	if err != nil || !objInfo.IsDir() {
		return false
	}
	refsInfo, err := fs.Stat(filepath.Join(path, "refs"))
	if err != nil || !refsInfo.IsDir() {
		return false
	}
	return true
}

func DetectRepoStructure(fs afero.Fs, path string) config.StructureType {
	// Hub directories created by cloneBareRepo are bare git repos with
	// metadata living directly under <path>/ (no .git subdir). Detect
	// that shape first: HEAD as a regular file plus objects/ and refs/
	// as directories. Without this branch, the .git-subdir check below
	// would mis-classify hubs as NotGit.
	if isBareRepoAtPath(fs, path) {
		return config.BareWorktreeRoot
	}

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

func GetCurrentWorktreeBranch(g git.GitInterface, path string) (string, error) {
	if !g.IsInsideWorkTree(path) {
		return "", fmt.Errorf("not inside a git worktree")
	}

	branch, err := g.GetCurrentBranch(path)
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}

	return branch, nil
}

func GetRepoRoot(g git.GitInterface, path string) (string, error) {
	if !g.IsInsideWorkTree(path) {
		return "", fmt.Errorf("not inside a git repository")
	}

	root, err := g.GetRoot(path)
	if err != nil {
		return "", fmt.Errorf("failed to get repo root: %w", err)
	}

	return root, nil
}

func ListWorktrees(g git.GitInterface, repoRoot string) ([]string, error) {
	out, err := g.RunInDir(repoRoot, "git", "worktree", "list", "--porcelain")
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

func GetWorktreePath(g git.GitInterface, repoRoot, branch string) (string, error) {
	out, err := g.RunInDir(repoRoot, "git", "worktree", "list", "--porcelain")
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
