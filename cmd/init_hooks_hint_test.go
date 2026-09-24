package cmd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"hop.top/git/internal/hooks"
)

func TestInitHooksHint_ListsOnlyRepoLevelDispatchedHooks(t *testing.T) {
	hint := strings.Join(initHooksHintLines(), "\n")

	for _, reserved := range []string{"env-start", "env-stop"} {
		assert.NotContains(t, hint, reserved, "hint advertises a hook that is never dispatched")
	}
	assert.NotContains(t, hint, "pre-clone", "pre-clone does not resolve at repo level")

	for _, name := range hooks.RepoLevelHookNames() {
		stem := strings.TrimPrefix(strings.TrimPrefix(name, "pre-"), "post-")
		if !strings.Contains(hint, name) && !strings.Contains(hint, "pre/post-"+stem) {
			t.Errorf("hint does not mention %s:\n%s", name, hint)
		}
	}
}

func TestInitHooksHint_FoldsPrePostPairs(t *testing.T) {
	hint := strings.Join(initHooksHintLines(), "\n")

	assert.Contains(t, hint, "pre/post-worktree-add")
	assert.Contains(t, hint, "post-clone")
	assert.NotContains(t, hint, "pre/post-clone")
}
