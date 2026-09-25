package hop

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A checkout that would overwrite local changes is got past by stashing
// them, and the hint says so. Any other failure has no hint: stashing
// would not help, and git's own message says what went wrong.
func TestUpdateRefusal_StashHintOnlyForOverwrite(t *testing.T) {
	overwrite := updateRefusal("/wt", errors.New("error: Your local changes to the following files would be overwritten by checkout:\n\tf.txt"))
	assert.Contains(t, overwrite.Msg, "cannot update the fork's worktree at /wt")
	assert.Equal(t, "stash the local changes (git -C /wt stash --include-untracked), then attach again", overwrite.Hint)

	other := updateRefusal("/wt", errors.New("fatal: Unable to create '/repo/index.lock': File exists."))
	assert.Contains(t, other.Msg, "index.lock")
	assert.Empty(t, other.Hint)
}
