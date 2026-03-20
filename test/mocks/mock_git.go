package mocks

import (
	"hop.top/git/internal/git"
)

// MockGit is a mock implementation of the Git interface for testing
type MockGit struct {
	Runner                *MockCommandRunner
	CreatedWorktrees      []string
	LastWorktreeBasePath  string
	LastWorktreeBranch    string
	DeletedLocalBranches  []string
	DeletedRemoteBranches []string
	RemoteBranchExists    bool
	RemoteBranches        []string
	MovedWorktrees        []string // tracks [oldPath, newPath] pairs flattened
	RenamedBranches       []string // tracks [oldBranch, newBranch] pairs flattened
}

// MockCommandRunner is a mock implementation of CommandRunner for testing
type MockCommandRunner struct {
	Responses map[string]string
	Errors    map[string]error
}

// Run executes a mocked command and returns configured responses or errors
func (m *MockCommandRunner) Run(cmd string, args ...string) (string, error) {
	key := cmd + " " + joinArgs(args)
	if m.Errors != nil && m.Errors[key] != nil {
		return "", m.Errors[key]
	}
	if m.Responses != nil {
		return m.Responses[key], nil
	}
	return "", nil
}

// RunInDir executes a mocked command in a specific directory
func (m *MockCommandRunner) RunInDir(dir string, cmd string, args ...string) (string, error) {
	key := dir + ":" + cmd + " " + joinArgs(args)
	if m.Errors != nil && m.Errors[key] != nil {
		return "", m.Errors[key]
	}
	if m.Responses != nil {
		return m.Responses[key], nil
	}
	return "", nil
}

// joinArgs concatenates command arguments into a single string
func joinArgs(args []string) string {
	result := ""
	for i, arg := range args {
		if i > 0 {
			result += " "
		}
		result += arg
	}
	return result
}

// Clone mocks the Clone operation
func (m *MockGit) Clone(uri, path, branch string) error {
	return nil
}

// CloneBare mocks the CloneBare operation
func (m *MockGit) CloneBare(uri, path string) error {
	return nil
}

// CreateWorktree mocks creating a worktree and tracks the operation
func (m *MockGit) CreateWorktree(hopspacePath, branch, path, base string, forceCreate bool) error {
	m.CreatedWorktrees = append(m.CreatedWorktrees, path)
	m.LastWorktreeBasePath = hopspacePath
	m.LastWorktreeBranch = branch
	return nil
}

// WorktreeRemove mocks removing a worktree
func (m *MockGit) WorktreeRemove(hopspacePath, path string, force bool) error {
	return nil
}

// WorktreePrune mocks pruning worktree information
func (m *MockGit) WorktreePrune(hopspacePath string) error {
	return nil
}

// RevParse mocks git rev-parse command
func (m *MockGit) RevParse(dir string, args ...string) (string, error) {
	return "", nil
}

// IsInsideWorkTree always returns true for mock
func (m *MockGit) IsInsideWorkTree(dir string) bool {
	return true
}

// GetRoot mocks getting the repository root
func (m *MockGit) GetRoot(dir string) (string, error) {
	return "", nil
}

// MergeBase mocks checking merge base between commits
func (m *MockGit) MergeBase(dir, commit1, commit2 string) (string, error) {
	return "", nil
}

// GetDefaultBranch mocks getting the default branch, always returns "main"
func (m *MockGit) GetDefaultBranch(uri string) (string, error) {
	return "main", nil
}

// GetCurrentRepo mocks getting current repository
func (m *MockGit) GetCurrentRepo() (string, error) {
	return "", nil
}

// GetRepoInfo mocks getting repository information
func (m *MockGit) GetRepoInfo() (uri, org, repo, branch string, err error) {
	return "", "", "", "", nil
}

// GetRemoteURL mocks getting remote URL
func (m *MockGit) GetRemoteURL(dir string) (string, error) {
	return "git@github.com:test-org/test-repo.git", nil
}

// GetCurrentBranch mocks getting current branch, always returns "main"
func (m *MockGit) GetCurrentBranch(dir string) (string, error) {
	return "main", nil
}

// GetStatus mocks getting repository status
func (m *MockGit) GetStatus(dir string) (*git.Status, error) {
	return &git.Status{
		Branch: "main",
		Clean:  true,
		Files:  []string{},
		Ahead:  0,
		Behind: 0,
	}, nil
}

// DeleteLocalBranch mocks deleting a local branch
func (m *MockGit) DeleteLocalBranch(dir, branch string) error {
	m.DeletedLocalBranches = append(m.DeletedLocalBranches, branch)
	return nil
}

// HasRemoteBranch mocks checking for a remote branch
func (m *MockGit) HasRemoteBranch(dir, branch string) bool {
	return m.RemoteBranchExists
}

// ListRemoteBranches mocks listing remote branches
func (m *MockGit) ListRemoteBranches(dir string) ([]string, error) {
	return m.RemoteBranches, nil
}

// DeleteRemoteBranch mocks deleting a remote branch
func (m *MockGit) DeleteRemoteBranch(dir, branch string) error {
	m.DeletedRemoteBranches = append(m.DeletedRemoteBranches, branch)
	return nil
}

// RunInDir executes a mocked command in a specific directory
func (m *MockGit) RunInDir(dir string, cmd string, args ...string) (string, error) {
	return m.Runner.RunInDir(dir, cmd, args...)
}

// Run executes a mocked command
func (m *MockGit) Run(cmd string, args ...string) (string, error) {
	return m.Runner.Run(cmd, args...)
}

// GetConfig mocks getting a git config value
func (m *MockGit) GetConfig(dir, key string) (string, error) {
	return m.Runner.RunInDir(dir, "git", "config", "--get", key)
}

// GetConfigRegex mocks getting git config values matching a regex
func (m *MockGit) GetConfigRegex(dir, pattern string) (map[string]string, error) {
	out, err := m.Runner.RunInDir(dir, "git", "config", "--get-regexp", pattern)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	if out == "" {
		return result, nil
	}
	return result, nil
}

// WorktreeMove mocks moving a worktree
func (m *MockGit) WorktreeMove(basePath, oldPath, newPath string) error {
	m.MovedWorktrees = append(m.MovedWorktrees, oldPath, newPath)
	return nil
}

// RenameBranch mocks renaming a local branch
func (m *MockGit) RenameBranch(dir, oldBranch, newBranch string) error {
	m.RenamedBranches = append(m.RenamedBranches, oldBranch, newBranch)
	return nil
}

// RunGitFlowStart mocks running git flow start
func (m *MockGit) RunGitFlowStart(dir, branchType, name string) error {
	_, err := m.Runner.RunInDir(dir, "git", "flow", branchType, "start", name)
	return err
}

// RunGitFlowFinish mocks running git flow finish
func (m *MockGit) RunGitFlowFinish(dir, branchType, name string) error {
	_, err := m.Runner.RunInDir(dir, "git", "flow", branchType, "finish", name)
	return err
}

// NewMockGit creates a new MockGit instance for testing
func NewMockGit() *MockGit {
	return &MockGit{
		Runner: &MockCommandRunner{
			Responses: make(map[string]string),
			Errors:    make(map[string]error),
		},
		CreatedWorktrees: []string{},
	}
}
