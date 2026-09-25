package services

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Hubs of one repository share a directory name more often than not
// (~/work/app, ~/tmp/app): the key must still tell them apart, stay the
// same for one hub, and be usable in a compose project name.
func TestHubKey_DistinctStableComposeSafe(t *testing.T) {
	root := t.TempDir()
	one := filepath.Join(root, "one", "My App")
	two := filepath.Join(root, "two", "My App")
	require.NoError(t, os.MkdirAll(one, 0o755))
	require.NoError(t, os.MkdirAll(two, 0o755))

	assert.NotEqual(t, HubKey(one), HubKey(two))
	assert.Equal(t, HubKey(one), HubKey(one+string(filepath.Separator)))
	assert.Regexp(t, regexp.MustCompile(`^my-app-[0-9a-f]{8}$`), HubKey(one))
}

func TestHubOverrideDir_PerHub(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	one := HubOverrideDir("org", "repo", "/x/one/app", "feat/a")
	two := HubOverrideDir("org", "repo", "/x/two/app", "feat/a")
	assert.NotEqual(t, one, two)
	assert.True(t, strings.HasSuffix(one, filepath.Join("org", "repo", HubKey("/x/one/app"), "feat", "a")), one)
	assert.NotEqual(t, legacyOverrideDir("org", "repo", "feat/a"), one)
}
