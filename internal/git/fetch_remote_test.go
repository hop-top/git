package git

import (
	"errors"
	"os/exec"
	"testing"
	"time"
)

// TestFetchRemote_BoundedByRemoteTimeout pins that FetchRemote runs under
// hop.remote.timeout: add fetches by default, so an unreachable origin must
// fail fast instead of hanging the command.
func TestFetchRemote_BoundedByRemoteTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("network stall test")
	}

	dir := initRepoWithConfig(t, "1")
	cmd := exec.Command("git", "-C", dir, "remote", "add", "origin",
		"git@10.255.255.1:example/repo.git")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("remote add: %v: %s", err, out)
	}

	start := time.Now()
	err := New().FetchRemote(dir, "origin")
	elapsed := time.Since(start)

	if !errors.Is(err, ErrNetworkTimeout) {
		t.Fatalf("got %v, want ErrNetworkTimeout", err)
	}
	if elapsed > 15*time.Second {
		t.Fatalf("took %v; deadline did not bound the fetch", elapsed)
	}
}
