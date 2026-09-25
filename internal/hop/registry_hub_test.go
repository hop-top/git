package hop_test

import (
	"reflect"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/hop"
)

// HubKeys names the entries that point into one hub: by worktree path
// inside the hub, by a worktree path the hub records elsewhere, or by
// project root. Another hub's entry for the same repository, or a hub
// whose path merely shares a prefix, is not among them.
func TestRegistry_HubKeys(t *testing.T) {
	withHumanMode(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fs := afero.NewMemMapFs()
	reg := `{"hops": {
		"acme/widget:main": {"repo": "acme/widget", "branch": "main", "path": "/hubs/one/hops/main"},
		"acme/widget:feat": {"repo": "acme/widget", "branch": "feat", "path": "/elsewhere/feat"},
		"acme/widget:dev":  {"repo": "acme/widget", "branch": "dev", "path": "/hubs/two/hops/dev"},
		"acme/gadget:main": {"repo": "acme/gadget", "branch": "main", "path": "/hubs/one-more/hops/main"},
		"acme/root:main":   {"repo": "acme/root", "branch": "main", "path": "/gone", "projectRoot": "/hubs/one"}
	}}`
	if err := afero.WriteFile(fs, hop.GetHopsRegistryPath(), []byte(reg), 0o644); err != nil {
		t.Fatal(err)
	}

	got := hop.LoadRegistry(fs).HubKeys("/hubs/one", []string{"/hubs/one/hops/main", "/elsewhere/feat"})
	want := []string{"acme/root:main", "acme/widget:feat", "acme/widget:main"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("HubKeys = %v, want %v", got, want)
	}
}

// A registry file that cannot be parsed is never written over: Save
// would otherwise replace every entry with what little was read.
func TestRegistry_SaveRefusesUnreadableFile(t *testing.T) {
	withHumanMode(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fs := afero.NewMemMapFs()
	path := hop.GetHopsRegistryPath()
	broken := `{"hops": [{"repo": "acme/widget"}]}`
	if err := afero.WriteFile(fs, path, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := hop.LoadRegistry(fs).AddHop("acme/widget", "main", "/hub/hops/main"); err == nil {
		t.Error("AddHop saved over an unreadable registry")
	}
	if got, _ := afero.ReadFile(fs, path); string(got) != broken {
		t.Errorf("registry rewritten to %s", got)
	}
}
