package hop

import (
	"testing"

	"hop.top/git/internal/config"
)

// hop.json holds absolute paths (written by `git hop add`) next to
// hub-relative ones (written by `git hop init`); each must resolve to the
// worktree itself, never to <hub>/<absolute path>.
func TestHubWorktreePaths_AbsoluteAndRelative(t *testing.T) {
	hub := &Hub{
		Path: "/w/hub",
		Config: &config.HubConfig{Branches: map[string]config.HubBranch{
			"feat/x": {Path: "/w/hub/hops/feat/x"},
			"main":   {Path: "hops/main"},
		}},
	}

	got := hub.WorktreePaths()

	want := map[string]string{
		"feat/x": "/w/hub/hops/feat/x",
		"main":   "/w/hub/hops/main",
	}
	if len(got) != len(want) {
		t.Fatalf("WorktreePaths() = %v, want %v", got, want)
	}
	for branch, path := range want {
		if got[branch] != path {
			t.Errorf("WorktreePaths()[%q] = %q, want %q", branch, got[branch], path)
		}
	}
}
