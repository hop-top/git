package hooks

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var envHookNames = []string{"pre-env-start", "post-env-start", "pre-env-stop", "post-env-stop"}

func TestIsDispatched_EnvHooksAreReserved(t *testing.T) {
	for _, name := range envHookNames {
		assert.False(t, IsDispatched(name), "%s is never dispatched", name)
		assert.NoError(t, ValidateHookName(name), "%s stays a valid name", name)
	}
}

func TestRepoLevelHookNames(t *testing.T) {
	got := RepoLevelHookNames()

	for _, name := range envHookNames {
		assert.NotContains(t, got, name)
	}
	assert.NotContains(t, got, "pre-clone", "pre-clone has no repo level")

	for _, name := range []string{
		"pre-worktree-add", "post-worktree-add",
		"pre-worktree-remove", "post-worktree-remove",
		"pre-worktree-move", "post-worktree-move",
		"pre-repair", "post-repair",
		"post-clone",
		"pre-worktree-switch", "post-worktree-switch",
	} {
		assert.Contains(t, got, name)
	}
}

// TestIsDispatched_MatchesCallSites keeps the dispatched/reserved split
// honest against the code: every dispatched name appears as a string
// literal somewhere outside this package, and no reserved name does.
func TestIsDispatched_MatchesCallSites(t *testing.T) {
	root := filepath.Join("..", "..")
	var src strings.Builder
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path == filepath.Join(root, "internal", "hooks") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			src.Write(b)
			return nil
		})
		require.NoError(t, err)
	}
	code := src.String()

	for _, name := range ValidHookNames {
		literal := `"` + name + `"`
		if IsDispatched(name) {
			assert.Contains(t, code, literal, "%s is marked dispatched but no call site names it", name)
		} else {
			assert.NotContains(t, code, literal, "%s is marked reserved but code names it", name)
		}
	}
}
