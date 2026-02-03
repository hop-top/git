package hop

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateErrorType_String(t *testing.T) {
	tests := []struct {
		name     string
		errType  StateErrorType
		expected string
	}{
		{
			name:     "OrphanedDirectory",
			errType:  OrphanedDirectory,
			expected: "OrphanedDirectory",
		},
		{
			name:     "PartialWorktree",
			errType:  PartialWorktree,
			expected: "PartialWorktree",
		},
		{
			name:     "ConfigMismatch",
			errType:  ConfigMismatch,
			expected: "ConfigMismatch",
		},
		{
			name:     "OrphanedGitMetadata",
			errType:  OrphanedGitMetadata,
			expected: "OrphanedGitMetadata",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.errType.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseGitError(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedOp     string
		expectedStderr string
		expectedCause  string
		shouldBeNil    bool
	}{
		{
			name:           "Valid git error with worktree operation",
			err:            fmt.Errorf("git command failed: git [worktree add /path/to/worktree branch]: exit status 128 (stderr: fatal: '/path/to/worktree' already exists)"),
			expectedOp:     "worktree add /path/to/worktree branch",
			expectedStderr: "fatal: '/path/to/worktree' already exists",
			expectedCause:  "exit status 128",
			shouldBeNil:    false,
		},
		{
			name:           "Valid git error with branch operation",
			err:            fmt.Errorf("git command failed: git [branch -b feature]: exit status 128 (stderr: fatal: A branch named 'feature' already exists.)"),
			expectedOp:     "branch -b feature",
			expectedStderr: "fatal: A branch named 'feature' already exists.",
			expectedCause:  "exit status 128",
			shouldBeNil:    false,
		},
		{
			name:           "Valid git error with clone operation",
			err:            fmt.Errorf("git command failed: git [clone --bare https://github.com/test/repo.git /path/to/repo]: exit status 128 (stderr: fatal: destination path '/path/to/repo' already exists and is not an empty directory.)"),
			expectedOp:     "clone --bare https://github.com/test/repo.git /path/to/repo",
			expectedStderr: "fatal: destination path '/path/to/repo' already exists and is not an empty directory.",
			expectedCause:  "exit status 128",
			shouldBeNil:    false,
		},
		{
			name:        "Non-git error",
			err:         fmt.Errorf("some other error"),
			shouldBeNil: true,
		},
		{
			name:        "Nil error",
			err:         nil,
			shouldBeNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseGitError(tt.err)

			if tt.shouldBeNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.expectedOp, result.Operation)
				assert.Equal(t, tt.expectedStderr, result.Stderr)
				assert.Equal(t, tt.expectedCause, result.Cause)
			}
		})
	}
}

func TestIsWorktreeExistsError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "Worktree already exists error",
			err:      fmt.Errorf("git command failed: git [worktree add /path/to/worktree branch]: exit status 128 (stderr: fatal: '/path/to/worktree' already exists)"),
			expected: true,
		},
		{
			name:     "Worktree already exists with different message format",
			err:      fmt.Errorf("git command failed: git [worktree add /path/to/worktree branch]: exit status 128 (stderr: fatal: worktree already exists)"),
			expected: true,
		},
		{
			name:     "Branch already exists error",
			err:      fmt.Errorf("git command failed: git [branch -b feature]: exit status 128 (stderr: fatal: A branch named 'feature' already exists.)"),
			expected: false,
		},
		{
			name:     "Different worktree error",
			err:      fmt.Errorf("git command failed: git [worktree add /path/to/worktree branch]: exit status 128 (stderr: fatal: invalid reference: branch)"),
			expected: false,
		},
		{
			name:     "Non-git error",
			err:      fmt.Errorf("some other error"),
			expected: false,
		},
		{
			name:     "Nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsWorktreeExistsError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsBranchExistsError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "Branch already exists error",
			err:      fmt.Errorf("git command failed: git [branch -b feature]: exit status 128 (stderr: fatal: A branch named 'feature' already exists.)"),
			expected: true,
		},
		{
			name:     "Branch already exists with different message format",
			err:      fmt.Errorf("git command failed: git [checkout -b feature]: exit status 128 (stderr: fatal: branch 'feature' already exists)"),
			expected: true,
		},
		{
			name:     "Worktree already exists error with branch in path",
			err:      fmt.Errorf("git command failed: git [worktree add /path/to/worktree branch]: exit status 128 (stderr: fatal: '/path/to/worktree' already exists)"),
			expected: false,
		},
		{
			name:     "Different branch error",
			err:      fmt.Errorf("git command failed: git [branch -b feature]: exit status 128 (stderr: fatal: Not a valid object name: 'main'.)"),
			expected: false,
		},
		{
			name:     "Non-git error",
			err:      fmt.Errorf("some other error"),
			expected: false,
		},
		{
			name:     "Nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsBranchExistsError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStateError_Error(t *testing.T) {
	err := &StateError{
		Type:    PartialWorktree,
		Path:    "/path/to/worktree",
		Message: "worktree directory exists but git metadata is missing",
	}

	expected := "PartialWorktree: /path/to/worktree: worktree directory exists but git metadata is missing"
	assert.Equal(t, expected, err.Error())
}

func TestGitError_Error(t *testing.T) {
	err := &GitError{
		Operation: "worktree add",
		Stderr:    "fatal: '/path/to/worktree' already exists",
		Cause:     "exit status 128",
	}

	result := err.Error()
	assert.Contains(t, result, "worktree add")
	assert.Contains(t, result, "fatal: '/path/to/worktree' already exists")
	assert.Contains(t, result, "exit status 128")
}

func TestGitError_Unwrap(t *testing.T) {
	baseErr := errors.New("exit status 128")
	err := &GitError{
		Operation: "worktree add",
		Stderr:    "fatal: '/path/to/worktree' already exists",
		Cause:     "exit status 128",
	}

	// Since we store Cause as a string, Unwrap should return nil
	// or we could store it as error type
	unwrapped := err.Unwrap()
	assert.Nil(t, unwrapped)
	_ = baseErr // suppress unused warning
}
