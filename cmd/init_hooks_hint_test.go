package cmd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"hop.top/git/internal/hooks"
)

// mentionsHook reports whether hint names hook, directly or folded into
// a "pre/post-<op>" pair.
func mentionsHook(hint, name string) bool {
	stem := strings.TrimPrefix(strings.TrimPrefix(name, "pre-"), "post-")
	for _, tok := range strings.FieldsFunc(hint, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
		if tok == name || tok == "pre/post-"+stem {
			return true
		}
	}
	return false
}

func TestInitHooksHint_HubDirListsEveryRepoLevelHook(t *testing.T) {
	hint := initHooksHint("/hub", false)

	for _, reserved := range []string{"env-start", "env-stop"} {
		assert.NotContains(t, hint, reserved, "hint advertises a hook that is never dispatched")
	}
	assert.False(t, mentionsHook(hint, "pre-clone"), "pre-clone does not resolve at repo level")
	for _, name := range hooks.RepoLevelHookNames() {
		assert.True(t, mentionsHook(hint, name), "hint does not mention %s:\n%s", name, hint)
	}
	assert.NotContains(t, hint, "/hub/.git-hop/hooks/", "hub dir needs no pointer elsewhere")
}

// In a bare conversion the hooks dir sits in hops/<branch>/. Hooks whose
// lookup never starts at a worktree (repair anchors on the hub;
// pre-worktree-add and post-worktree-remove run while the worktree is
// absent) are not offered there; the hint sends them to the hub's dir.
func TestInitHooksHint_WorktreeDirSendsHubOnlyHooksToHub(t *testing.T) {
	hint := initHooksHint("/hub", true)

	here, elsewhere, ok := strings.Cut(hint, "/hub/.git-hop/hooks/")
	if !ok {
		t.Fatalf("hint does not name the hub hooks dir:\n%s", hint)
	}
	for _, name := range hooks.HubOnlyHookNames() {
		assert.False(t, mentionsHook(here, name), "worktree dir offered %s, which is never looked up there:\n%s", name, hint)
		assert.True(t, mentionsHook(elsewhere, name), "hint does not say where %s must live:\n%s", name, hint)
	}
	for _, name := range hooks.WorktreeLevelHookNames() {
		assert.True(t, mentionsHook(here, name), "hint does not offer %s:\n%s", name, hint)
	}
}

func TestInitHooksHint_FoldsPrePostPairs(t *testing.T) {
	hint := initHooksHint("/hub", false)

	assert.Contains(t, hint, "pre/post-worktree-add")
	assert.Contains(t, hint, "post-clone")
	assert.NotContains(t, hint, "pre/post-clone")
}
