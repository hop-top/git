package hop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/hop"
)

// hops.json written by older versions carries followsConvention, which
// git-hop never read. It must still load, without a parse warning.
func TestLoadRegistry_OldFileWithFollowsConvention(t *testing.T) {
	withHumanMode(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := hop.GetHopsRegistryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{"hops": {"acme/widget:main": {"repo": "acme/widget", "branch": "main",
		"path": "/hub/hops/main", "envState": "none", "followsConvention": true}}}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	var r *hop.Registry
	stdout, stderr := captureStreams(t, func() { r = hop.LoadRegistry(afero.NewOsFs()) })
	if stdout != "" || stderr != "" {
		t.Errorf("LoadRegistry output: stdout=%q stderr=%q, want none", stdout, stderr)
	}
	e, ok := r.Config.Hops["acme/widget:main"]
	if !ok || e.Path != "/hub/hops/main" || e.Repo != "acme/widget" {
		t.Errorf("registry entry = %+v (present=%v)", e, ok)
	}
}
