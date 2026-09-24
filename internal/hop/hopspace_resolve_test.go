package hop_test

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/hop"
)

// A default clone never writes a data-home hopspace: its hub hop.json is
// the hopspace. Only a --global clone creates <data>/<org>/<repo>/hop.json.
func TestResolveHopspacePath(t *testing.T) {
	t.Setenv("GIT_HOP_DATA_HOME", "/data")
	hubPath := "/work/hub"
	global := filepath.Join("/data", "org", "repo")

	t.Run("local hub resolves to the hub", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, filepath.Join(hubPath, "hop.json"), []byte("{}"), 0o644))

		assert.Equal(t, hubPath, hop.ResolveHopspacePath(fs, hubPath, "org", "repo"))
	})

	t.Run("data-home hopspace wins when it has a hop.json", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, filepath.Join(hubPath, "hop.json"), []byte("{}"), 0o644))
		require.NoError(t, afero.WriteFile(fs, filepath.Join(global, "hop.json"), []byte("{}"), 0o644))

		assert.Equal(t, global, hop.ResolveHopspacePath(fs, hubPath, "org", "repo"))
	})

	t.Run("a data-home directory without hop.json is not a hopspace", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll(filepath.Join(global, "deps"), 0o755))

		assert.Equal(t, hubPath, hop.ResolveHopspacePath(fs, hubPath, "org", "repo"))
	})
}
