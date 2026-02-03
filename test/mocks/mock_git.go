package mocks

type MockGit struct {
	Runner                *MockCommandRunner
	CreatedWorktrees      []string // Track created worktree paths
	LastWorktreeBasePath  string   // Track the base path used for worktree commands
	LastWorktreeBranch    string   // Track the branch name used
}

type MockCommandRunner struct {
	Responses map[string]string
	Errors    map[string]error
}

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

func (m *MockGit) Clone(uri, path, branch string) error {
	return nil
}

func (m *MockGit) CloneBare(uri, path string) error {
	return nil
}

func (m *MockGit) WorktreeAdd(hopspacePath, branch, path string) error {
	m.CreatedWorktrees = append(m.CreatedWorktrees, path)
	m.LastWorktreeBasePath = hopspacePath
	m.LastWorktreeBranch = branch
	return nil
}

func (m *MockGit) WorktreeAddCreate(hopspacePath, branch, path, base string) error {
	m.CreatedWorktrees = append(m.CreatedWorktrees, path)
	m.LastWorktreeBasePath = hopspacePath
	m.LastWorktreeBranch = branch
	return nil
}

func (m *MockGit) WorktreeRemove(hopspacePath, path string, force bool) error {
	return nil
}

func (m *MockGit) WorktreePrune(hopspacePath string) error {
	return nil
}

func (m *MockGit) RevParse(dir string, args ...string) (string, error) {
	return "", nil
}

func (m *MockGit) IsInsideWorkTree(dir string) bool {
	return true
}

func (m *MockGit) GetRoot(dir string) (string, error) {
	return "", nil
}

func (m *MockGit) MergeBase(dir, commit1, commit2 string) (string, error) {
	return "", nil
}

func (m *MockGit) GetDefaultBranch(uri string) (string, error) {
	return "main", nil
}

func (m *MockGit) GetCurrentRepo() (string, error) {
	return "", nil
}

func (m *MockGit) GetRepoInfo() (uri, org, repo, branch string, err error) {
	return "", "", "", "", nil
}

func (m *MockGit) GetRemoteURL(dir string) (string, error) {
	return "git@github.com:test-org/test-repo.git", nil
}

func (m *MockGit) GetCurrentBranch(dir string) (string, error) {
	return "main", nil
}

func (m *MockGit) GetBranches(dir string) ([]string, error) {
	return []string{"main", "develop"}, nil
}

func NewMockGit() *MockGit {
	return &MockGit{
		Runner: &MockCommandRunner{
			Responses: make(map[string]string),
			Errors:    make(map[string]error),
		},
		CreatedWorktrees: []string{},
	}
}
