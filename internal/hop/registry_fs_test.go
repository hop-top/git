package hop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/hop"
)

// The registry reads and writes through the one filesystem it is given:
// what Save wrote, the next LoadRegistry on that filesystem reads back.
func TestRegistry_RoundTripsThroughInjectedFs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fs := afero.NewMemMapFs()

	if err := hop.LoadRegistry(fs).AddHop("acme/widget", "main", "/hub/hops/main"); err != nil {
		t.Fatalf("AddHop: %v", err)
	}

	e, ok := hop.LoadRegistry(fs).Config.Hops["acme/widget:main"]
	if !ok || e.Path != "/hub/hops/main" {
		t.Errorf("reloaded entry = %+v (present=%v), want the saved hop", e, ok)
	}
}

// A registry on the injected filesystem is the one loaded, not whatever
// sits at the same path on the real disk.
func TestLoadRegistry_ReadsInjectedFsNotDisk(t *testing.T) {
	withHumanMode(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := hop.GetHopsRegistryPath()

	onDisk := `{"hops": {"disk/only:main": {"repo": "disk/only", "branch": "main", "path": "/disk"}}}`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(onDisk), 0o644); err != nil {
		t.Fatal(err)
	}

	fs := afero.NewMemMapFs()
	injected := `{"hops": {"acme/widget:main": {"repo": "acme/widget", "branch": "main", "path": "/mem"}}}`
	if err := afero.WriteFile(fs, path, []byte(injected), 0o644); err != nil {
		t.Fatal(err)
	}

	hops := hop.LoadRegistry(fs).Config.Hops
	if _, ok := hops["disk/only:main"]; ok {
		t.Error("LoadRegistry read the real disk instead of the injected filesystem")
	}
	if e, ok := hops["acme/widget:main"]; !ok || e.Path != "/mem" {
		t.Errorf("injected entry = %+v (present=%v)", e, ok)
	}
}
