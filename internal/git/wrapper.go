package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// GitInterface defines the interface for git operations
type GitInterface interface {
	Clone(uri, path, branch string) error
	CloneBare(uri, path string) error
	CreateWorktree(hopspacePath, branch, path, base string, forceCreate bool) error
	WorktreeRemove(hopspacePath, path string, force bool) error
	WorktreePrune(hopspacePath string) error
	RevParse(dir string, args ...string) (string, error)
	IsInsideWorkTree(dir string) bool
	GetRoot(dir string) (string, error)
	MergeBase(dir, commit1, commit2 string) (string, error)
	GetDefaultBranch(uri string) (string, error)
	GetCurrentRepo() (string, error)
	GetRepoInfo() (uri, org, repo, branch string, err error)
	GetRemoteURL(dir string) (string, error)
	GetCurrentBranch(dir string) (string, error)
	GetStatus(dir string) (*Status, error)
	DeleteLocalBranch(dir, branch string) error
	HasRemoteBranch(dir, branch string) bool
	DeleteRemoteBranch(dir, branch string) error
	ListRemoteBranches(dir string) ([]string, error)
	RunInDir(dir string, cmd string, args ...string) (string, error)
	Run(cmd string, args ...string) (string, error)
}

// Git wraps git command execution
type Git struct {
	Runner CommandRunner
}

// CommandRunner interface for mocking
type CommandRunner interface {
	Run(cmd string, args ...string) (string, error)
	RunInDir(dir string, cmd string, args ...string) (string, error)
}

// RealRunner implements CommandRunner using os/exec
type RealRunner struct{}

// Run executes a command in the current directory
func (r *RealRunner) Run(cmd string, args ...string) (string, error) {
	return r.RunInDir("", cmd, args...)
}

// RunInDir executes a command in the specified directory.
// If dir is empty, runs in the current directory.
func (r *RealRunner) RunInDir(dir string, cmd string, args ...string) (string, error) {
	c := exec.Command(cmd, args...)
	if dir != "" {
		c.Dir = dir
	}
	c.Env = os.Environ()
	c.Env = append(c.Env, "GIT_TERMINAL_PROMPT=0")

	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	err := c.Run()
	if err != nil {
		return stdout.String(), fmt.Errorf("git command failed: %s %v: %s (stderr: %s)", cmd, args, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// New creates a new Git wrapper
func New() *Git {
	return &Git{Runner: &RealRunner{}}
}

// Clone clones a repository
func (g *Git) Clone(uri, path, branch string) error {
	args := []string{"clone", "--single-branch"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, uri, path)
	_, err := g.Runner.Run("git", args...)
	return err
}

// CloneBare clones a repository as a bare repository
func (g *Git) CloneBare(uri, path string) error {
	args := []string{"clone", "--bare", uri, path}
	_, err := g.Runner.Run("git", args...)
	return err
}

// CreateWorktree creates a new worktree.
// If the branch exists and forceCreate is false, it links the existing branch.
// If the branch doesn't exist or forceCreate is true, it creates a new branch from base (or HEAD if base is empty).
func (g *Git) CreateWorktree(hopspacePath, branch, path, base string, forceCreate bool) error {
	args := []string{"worktree", "add"}

	if forceCreate {
		// Force create a new branch
		args = append(args, "-b", branch, path)
		if base != "" {
			args = append(args, base)
		}
	} else {
		// Try to link existing branch, or create if it doesn't exist
		args = append(args, path, branch)
	}

	_, err := g.Runner.RunInDir(hopspacePath, "git", args...)

	// If linking failed because branch doesn't exist, try creating it
	if err != nil && !forceCreate {
		// Check if the branch already exists before using -b
		_, branchExists := g.Runner.RunInDir(hopspacePath, "git", "rev-parse", "--verify", "refs/heads/"+branch)
		if branchExists != nil {
			// Branch doesn't exist, create it with -b
			args = []string{"worktree", "add", "-b", branch, path}
			if base != "" {
				args = append(args, base)
			}
			_, err = g.Runner.RunInDir(hopspacePath, "git", args...)
		}
		// If branch exists but worktree add failed, the original error stands
	}

	return err
}

// WorktreeRemove removes a worktree
func (g *Git) WorktreeRemove(hopspacePath, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	_, err := g.Runner.RunInDir(hopspacePath, "git", args...)
	return err
}

// WorktreePrune prunes worktree information
func (g *Git) WorktreePrune(hopspacePath string) error {
	_, err := g.Runner.RunInDir(hopspacePath, "git", "worktree", "prune")
	return err
}

// RevParse returns the output of git rev-parse
func (g *Git) RevParse(dir string, args ...string) (string, error) {
	return g.Runner.RunInDir(dir, "git", append([]string{"rev-parse"}, args...)...)
}

// IsInsideWorkTree checks if we are inside a git worktree
func (g *Git) IsInsideWorkTree(dir string) bool {
	out, err := g.RevParse(dir, "--is-inside-work-tree")
	return err == nil && out == "true"
}

// GetRoot returns the root of the current git repo
func (g *Git) GetRoot(dir string) (string, error) {
	return g.RevParse(dir, "--show-toplevel")
}

// MergeBase checks if a merge base exists between two commits
func (g *Git) MergeBase(dir, commit1, commit2 string) (string, error) {
	return g.Runner.RunInDir(dir, "git", "merge-base", commit1, commit2)
}

// GetDefaultBranch returns the default branch for a remote URI using git ls-remote.
// Parses the symref output to extract the branch name from refs/heads/.
func (g *Git) GetDefaultBranch(uri string) (string, error) {
	out, err := g.Runner.Run("git", "ls-remote", "--symref", uri, "HEAD")
	if err != nil {
		return "", err
	}
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[0] == "ref:" {
			ref := parts[1]
			return strings.TrimPrefix(ref, "refs/heads/"), nil
		}
	}
	return "", fmt.Errorf("could not determine default branch")
}

// GetCurrentRepo returns org/repo for current working directory
func (g *Git) GetCurrentRepo() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	if !g.IsInsideWorkTree(cwd) {
		return "", fmt.Errorf("not inside a git repository")
	}

	remoteURL, err := g.GetRemoteURL(cwd)
	if err != nil {
		return "", err
	}

	org, repo := parseRepoFromURL(remoteURL)
	if org == "" || repo == "" {
		return "", fmt.Errorf("could not parse repo from URL: %s", remoteURL)
	}

	return org + "/" + repo, nil
}

// GetRepoInfo returns full repo information (uri, org, repo, branch)
func (g *Git) GetRepoInfo() (uri, org, repo, branch string, err error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", "", "", err
	}

	if !g.IsInsideWorkTree(cwd) {
		return "", "", "", "", fmt.Errorf("not inside a git repository")
	}

	uri, err = g.GetRemoteURL(cwd)
	if err != nil {
		return "", "", "", "", err
	}

	org, repo = parseRepoFromURL(uri)
	branch, err = g.GetCurrentBranch(cwd)

	return uri, org, repo, branch, err
}

// GetRemoteURL returns the git remote URL for the current repository
func (g *Git) GetRemoteURL(dir string) (string, error) {
	out, err := g.Runner.RunInDir(dir, "git", "remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("no remote 'origin' found: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// GetCurrentBranch returns the current branch name
func (g *Git) GetCurrentBranch(dir string) (string, error) {
	out, err := g.Runner.RunInDir(dir, "git", "branch", "--show-current")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// DeleteLocalBranch force-deletes a local branch
func (g *Git) DeleteLocalBranch(dir, branch string) error {
	_, err := g.Runner.RunInDir(dir, "git", "branch", "-D", branch)
	return err
}

// HasRemoteBranch checks whether a branch exists on the remote
func (g *Git) HasRemoteBranch(dir, branch string) bool {
	out, err := g.Runner.RunInDir(dir, "git", "ls-remote", "--heads", "origin", branch)
	return err == nil && out != ""
}

// DeleteRemoteBranch deletes a branch from the remote
func (g *Git) DeleteRemoteBranch(dir, branch string) error {
	_, err := g.Runner.RunInDir(dir, "git", "push", "origin", "--delete", branch)
	return err
}

// ListRemoteBranches returns remote branch names with the origin/ prefix stripped.
// Filters out HEAD pointer lines.
func (g *Git) ListRemoteBranches(dir string) ([]string, error) {
	out, err := g.Runner.RunInDir(dir, "git", "branch", "-r")
	if err != nil {
		return nil, err
	}
	var branches []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "->") {
			continue
		}
		branch := strings.TrimPrefix(line, "origin/")
		branches = append(branches, branch)
	}
	sort.Strings(branches)
	return branches, nil
}

// RunInDir executes a command in the specified directory
func (g *Git) RunInDir(dir string, cmd string, args ...string) (string, error) {
	return g.Runner.RunInDir(dir, cmd, args...)
}

// Run executes a command in the current directory
func (g *Git) Run(cmd string, args ...string) (string, error) {
	return g.Runner.Run(cmd, args...)
}

// parseRepoFromURL parses org and repo name from various git URL formats.
// Supports file://, git@, and https:// URL formats.
// Returns empty strings if the URL format is unrecognized.
func parseRepoFromURL(uri string) (org, repo string) {
	trimmed := strings.TrimSuffix(uri, ".git")

	if strings.HasPrefix(trimmed, "file://") {
		path := strings.TrimPrefix(trimmed, "file://")
		parts := strings.Split(path, "/")
		var nonEmpty []string
		for _, p := range parts {
			if p != "" {
				nonEmpty = append(nonEmpty, p)
			}
		}
		if len(nonEmpty) >= 2 {
			return nonEmpty[len(nonEmpty)-2], nonEmpty[len(nonEmpty)-1]
		}
		if len(nonEmpty) == 1 {
			return nonEmpty[0], nonEmpty[0]
		}
		return "", ""
	}

	if strings.HasPrefix(trimmed, "git@") {
		parts := strings.Split(trimmed, ":")
		if len(parts) == 2 {
			path := parts[1]
			pathParts := strings.Split(path, "/")
			if len(pathParts) >= 2 {
				return pathParts[len(pathParts)-2], pathParts[len(pathParts)-1]
			}
		}
	}

	if strings.Contains(trimmed, "://") {
		parts := strings.Split(trimmed, "/")
		if len(parts) >= 2 {
			return parts[len(parts)-2], parts[len(parts)-1]
		}
	}

	return "", ""
}
