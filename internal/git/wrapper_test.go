package git

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCreateWorktree_LinkExistingBranch tests linking an existing branch
func TestCreateWorktree_LinkExistingBranch(t *testing.T) {
	runner := &MockRunner{
		responses: make(map[string]string),
		errors:    make(map[string]error),
	}
	g := &Git{Runner: runner}

	// Success case - existing branch links successfully
	err := g.CreateWorktree("/hub", "existing-branch", "/path/to/worktree", "", false)
	assert.NoError(t, err)
	assert.Equal(t, 1, runner.callCount, "Should call git once")
	assert.Contains(t, runner.lastCommand, "worktree add /path/to/worktree existing-branch")
}

// TestCreateWorktree_CreateNewBranch tests creating a new branch when linking fails
func TestCreateWorktree_CreateNewBranch(t *testing.T) {
	runner := &MockRunner{
		responses: make(map[string]string),
		errors:    make(map[string]error),
	}
	g := &Git{Runner: runner}

	// First call fails (branch doesn't exist), second call creates it
	runner.errors["/hub:git worktree add /path/to/worktree new-branch"] = fmt.Errorf("branch not found")

	err := g.CreateWorktree("/hub", "new-branch", "/path/to/worktree", "HEAD", false)
	assert.NoError(t, err)
	assert.Equal(t, 2, runner.callCount, "Should call git twice - link attempt then create")
	assert.Contains(t, runner.lastCommand, "worktree add -b new-branch /path/to/worktree HEAD")
}

// TestCreateWorktree_CreateNewBranchFromBase tests creating with custom base
func TestCreateWorktree_CreateNewBranchFromBase(t *testing.T) {
	runner := &MockRunner{
		responses: make(map[string]string),
		errors:    make(map[string]error),
	}
	g := &Git{Runner: runner}

	// First call fails, second call creates with custom base
	runner.errors["/hub:git worktree add /path/to/worktree feature-branch"] = fmt.Errorf("branch not found")

	err := g.CreateWorktree("/hub", "feature-branch", "/path/to/worktree", "develop", false)
	assert.NoError(t, err)
	assert.Equal(t, 2, runner.callCount)
	assert.Contains(t, runner.lastCommand, "worktree add -b feature-branch /path/to/worktree develop")
}

// TestCreateWorktree_ForceCreate tests forcing branch creation
func TestCreateWorktree_ForceCreate(t *testing.T) {
	runner := &MockRunner{
		responses: make(map[string]string),
		errors:    make(map[string]error),
	}
	g := &Git{Runner: runner}

	// With forceCreate=true, should create directly without trying to link
	err := g.CreateWorktree("/hub", "forced-branch", "/path/to/worktree", "HEAD", true)
	assert.NoError(t, err)
	assert.Equal(t, 1, runner.callCount, "Should call git once with -b flag")
	assert.Contains(t, runner.lastCommand, "worktree add -b forced-branch /path/to/worktree HEAD")
}

// TestCreateWorktree_ForceCreateWithoutBase tests forcing creation without base
func TestCreateWorktree_ForceCreateWithoutBase(t *testing.T) {
	runner := &MockRunner{
		responses: make(map[string]string),
		errors:    make(map[string]error),
	}
	g := &Git{Runner: runner}

	err := g.CreateWorktree("/hub", "forced-branch", "/path/to/worktree", "", true)
	assert.NoError(t, err)
	assert.Equal(t, 1, runner.callCount)
	assert.Contains(t, runner.lastCommand, "worktree add -b forced-branch /path/to/worktree")
	assert.NotContains(t, runner.lastCommand, "HEAD", "Should not include base when empty")
}

// TestCreateWorktree_BothCallsFail tests when both link and create fail
func TestCreateWorktree_BothCallsFail(t *testing.T) {
	runner := &MockRunner{
		responses: make(map[string]string),
		errors:    make(map[string]error),
	}
	g := &Git{Runner: runner}

	// Both calls fail
	runner.errors["/hub:git worktree add /path/to/worktree bad-branch"] = fmt.Errorf("branch not found")
	runner.errors["/hub:git worktree add -b bad-branch /path/to/worktree HEAD"] = fmt.Errorf("permission denied")

	err := g.CreateWorktree("/hub", "bad-branch", "/path/to/worktree", "HEAD", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
	assert.Equal(t, 2, runner.callCount, "Should try both commands")
}

// TestCreateWorktree_ForceCreateFails tests when forced creation fails
func TestCreateWorktree_ForceCreateFails(t *testing.T) {
	runner := &MockRunner{
		responses: make(map[string]string),
		errors:    make(map[string]error),
	}
	g := &Git{Runner: runner}

	runner.errors["/hub:git worktree add -b fail-branch /path/to/worktree HEAD"] = fmt.Errorf("disk full")

	err := g.CreateWorktree("/hub", "fail-branch", "/path/to/worktree", "HEAD", true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")
	assert.Equal(t, 1, runner.callCount, "Should only try once with forceCreate")
}

// TestCreateWorktree_EmptyParameters tests with empty/invalid parameters
func TestCreateWorktree_EmptyParameters(t *testing.T) {
	runner := &MockRunner{
		responses: make(map[string]string),
		errors:    make(map[string]error),
	}
	g := &Git{Runner: runner}

	// Should still attempt the command even with empty parameters
	// (git will handle validation)
	err := g.CreateWorktree("", "", "", "", false)
	assert.NoError(t, err) // Mock doesn't validate parameters
}

// MockRunner is a test helper for mocking git commands
type MockRunner struct {
	responses   map[string]string
	errors      map[string]error
	callCount   int
	lastCommand string
}

func (m *MockRunner) Run(cmd string, args ...string) (string, error) {
	return m.RunInDir("", cmd, args...)
}

func (m *MockRunner) RunInDir(dir string, cmd string, args ...string) (string, error) {
	m.callCount++

	// Build key for lookups
	key := dir + ":" + cmd + " " + strings.Join(args, " ")
	m.lastCommand = key

	if err, exists := m.errors[key]; exists {
		return "", err
	}

	if resp, exists := m.responses[key]; exists {
		return resp, nil
	}

	return "", nil
}
