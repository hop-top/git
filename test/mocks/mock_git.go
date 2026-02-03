package mocks

// MockGit is a mock implementation of the Git interface for testing
type MockGit struct {
	Runner               *MockCommandRunner
	CreatedWorktrees     []string
	LastWorktreeBasePath string
	LastWorktreeBranch   string
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

// GetBranches mocks getting repository branches
func (m *MockGit) GetBranches(dir string) ([]string, error) {
	return []string{"main", "develop"}, nil
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
