package cmd

import (
	"os/exec"
	"path/filepath"
	"testing"

	"hop.top/git/internal/git"
	"hop.top/git/internal/git/gittrace"
)

// add fetches origin and then adds the worktree, so its fetch must not
// start git's auto-maintenance either: since git 2.54 that detached run
// prunes the new worktrees/<name> entry if it lands before add locks it.
func TestFetchOrigin_StartsNoAutoMaintenance(t *testing.T) {
	dir := t.TempDir()
	upstream := filepath.Join(dir, "upstream")
	hub := filepath.Join(dir, "hub")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main", upstream},
		{"-C", upstream, "commit", "-q", "--allow-empty", "-m", "init"},
		{"clone", "-q", "--bare", upstream, hub},
		{"-C", hub, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	trace := gittrace.Start(t)
	fetchOrigin(git.New(), hub, fetchRequired)
	trace.RequireNoAutoMaintenance(t)
}
