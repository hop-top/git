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

	// First call fails (branch doesn't exist), rev-parse confirms it doesn't exist, then create
	runner.errors["/hub:git worktree add /path/to/worktree new-branch"] = fmt.Errorf("branch not found")
	runner.errors["/hub:git rev-parse --verify refs/heads/new-branch"] = fmt.Errorf("not a valid ref")

	err := g.CreateWorktree("/hub", "new-branch", "/path/to/worktree", "HEAD", false)
	assert.NoError(t, err)
	assert.Equal(t, 3, runner.callCount, "Should call git three times - link attempt, rev-parse check, then create")
	assert.Contains(t, runner.lastCommand, "worktree add -b new-branch /path/to/worktree HEAD")
}

// TestCreateWorktree_CreateNewBranchFromBase tests creating with custom base
func TestCreateWorktree_CreateNewBranchFromBase(t *testing.T) {
	runner := &MockRunner{
		responses: make(map[string]string),
		errors:    make(map[string]error),
	}
	g := &Git{Runner: runner}

	// First call fails, rev-parse confirms branch doesn't exist, then creates with custom base
	runner.errors["/hub:git worktree add /path/to/worktree feature-branch"] = fmt.Errorf("branch not found")
	runner.errors["/hub:git rev-parse --verify refs/heads/feature-branch"] = fmt.Errorf("not a valid ref")

	err := g.CreateWorktree("/hub", "feature-branch", "/path/to/worktree", "develop", false)
	assert.NoError(t, err)
	assert.Equal(t, 3, runner.callCount)
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

	// All calls fail: link attempt, rev-parse shows branch doesn't exist, create also fails
	runner.errors["/hub:git worktree add /path/to/worktree bad-branch"] = fmt.Errorf("branch not found")
	runner.errors["/hub:git rev-parse --verify refs/heads/bad-branch"] = fmt.Errorf("not a valid ref")
	runner.errors["/hub:git worktree add -b bad-branch /path/to/worktree HEAD"] = fmt.Errorf("permission denied")

	err := g.CreateWorktree("/hub", "bad-branch", "/path/to/worktree", "HEAD", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
	assert.Equal(t, 3, runner.callCount, "Should try link, rev-parse, then create")
}

// TestCreateWorktree_BranchExistsButCheckedOut tests when branch exists but is already checked out
func TestCreateWorktree_BranchExistsButCheckedOut(t *testing.T) {
	runner := &MockRunner{
		responses: make(map[string]string),
		errors:    make(map[string]error),
	}
	g := &Git{Runner: runner}

	// Link fails because branch is checked out in another worktree
	runner.errors["/hub:git worktree add /path/to/worktree my-branch"] = fmt.Errorf("branch 'my-branch' is already checked out")
	// rev-parse succeeds — branch exists, so we should NOT try -b
	// (default mock returns no error)

	err := g.CreateWorktree("/hub", "my-branch", "/path/to/worktree", "HEAD", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already checked out")
	assert.Equal(t, 2, runner.callCount, "Should try link and rev-parse, but not -b since branch exists")
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

// TestDeleteLocalBranch tests force-deleting a local branch
func TestDeleteLocalBranch(t *testing.T) {
	runner := &MockRunner{
		responses: make(map[string]string),
		errors:    make(map[string]error),
	}
	g := &Git{Runner: runner}

	err := g.DeleteLocalBranch("/repo", "feature")
	assert.NoError(t, err)
	assert.Contains(t, runner.lastCommand, "branch -D feature")
}

// TestDeleteLocalBranch_Error tests failure when branch doesn't exist
func TestDeleteLocalBranch_Error(t *testing.T) {
	runner := &MockRunner{
		responses: make(map[string]string),
		errors:    make(map[string]error),
	}
	g := &Git{Runner: runner}

	runner.errors["/repo:git branch -D gone"] = fmt.Errorf("branch 'gone' not found")
	err := g.DeleteLocalBranch("/repo", "gone")
	assert.Error(t, err)
}

// TestHasRemoteBranch_Exists tests detecting an existing remote branch
func TestHasRemoteBranch_Exists(t *testing.T) {
	runner := &MockRunner{
		responses: make(map[string]string),
		errors:    make(map[string]error),
	}
	g := &Git{Runner: runner}

	runner.responses["/repo:git ls-remote --heads origin feature"] = "abc123\trefs/heads/feature"
	assert.True(t, g.HasRemoteBranch("/repo", "feature"))
}

// TestHasRemoteBranch_NotExists tests detecting a missing remote branch
func TestHasRemoteBranch_NotExists(t *testing.T) {
	runner := &MockRunner{
		responses: make(map[string]string),
		errors:    make(map[string]error),
	}
	g := &Git{Runner: runner}

	// Default empty response means branch doesn't exist
	assert.False(t, g.HasRemoteBranch("/repo", "nope"))
}

// TestDeleteRemoteBranch tests deleting a remote branch
func TestDeleteRemoteBranch(t *testing.T) {
	runner := &MockRunner{
		responses: make(map[string]string),
		errors:    make(map[string]error),
	}
	g := &Git{Runner: runner}

	err := g.DeleteRemoteBranch("/repo", "feature")
	assert.NoError(t, err)
	assert.Contains(t, runner.lastCommand, "push origin --delete feature")
}

// TestDeleteRemoteBranch_Error tests failure when remote delete fails
func TestDeleteRemoteBranch_Error(t *testing.T) {
	runner := &MockRunner{
		responses: make(map[string]string),
		errors:    make(map[string]error),
	}
	g := &Git{Runner: runner}

	runner.errors["/repo:git push origin --delete feature"] = fmt.Errorf("remote rejected")
	err := g.DeleteRemoteBranch("/repo", "feature")
	assert.Error(t, err)
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
