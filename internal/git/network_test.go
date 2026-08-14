package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// initRepoWithConfig creates a throwaway repo, optionally setting
// hop.remote.timeout locally, and returns its path.
func initRepoWithConfig(t *testing.T, timeout string) string {
	t.Helper()

	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	if timeout != "" {
		run("config", networkTimeoutConfigKey, timeout)
	}
	return dir
}

// TestNetworkTimeout_Default verifies the compiled default applies when
// the key is unset.
func TestNetworkTimeout_Default(t *testing.T) {
	dir := initRepoWithConfig(t, "")
	if got := networkTimeout(dir); got != DefaultNetworkTimeout {
		t.Fatalf("got %v, want %v", got, DefaultNetworkTimeout)
	}
}

// TestNetworkTimeout_Override verifies a per-repo override is honored.
func TestNetworkTimeout_Override(t *testing.T) {
	dir := initRepoWithConfig(t, "3")
	if got := networkTimeout(dir); got != 3*time.Second {
		t.Fatalf("got %v, want 3s", got)
	}
}

// TestNetworkTimeout_Disabled verifies 0 disables the deadline.
func TestNetworkTimeout_Disabled(t *testing.T) {
	dir := initRepoWithConfig(t, "0")
	if got := networkTimeout(dir); got != 0 {
		t.Fatalf("got %v, want 0", got)
	}
}

// TestNetworkTimeout_InvalidFallsBackToDefault verifies a garbage value
// does not disable or shorten the deadline.
func TestNetworkTimeout_InvalidFallsBackToDefault(t *testing.T) {
	for _, v := range []string{"soon", "-5"} {
		dir := initRepoWithConfig(t, v)
		if got := networkTimeout(dir); got != DefaultNetworkTimeout {
			t.Fatalf("%q: got %v, want %v", v, got, DefaultNetworkTimeout)
		}
	}
}

// TestRunNetwork_TimesOut is the deadline's regression test: a remote
// that never answers must fail fast with ErrNetworkTimeout rather than
// block forever. 10.255.255.1 is unroutable, so the TCP connect hangs
// until the deadline fires.
func TestRunNetwork_TimesOut(t *testing.T) {
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
	_, err := runNetwork(dir, "ls-remote", "--heads", "origin", "main")
	elapsed := time.Since(start)

	if !errors.Is(err, ErrNetworkTimeout) {
		t.Fatalf("got %v, want ErrNetworkTimeout", err)
	}
	if elapsed > 15*time.Second {
		t.Fatalf("took %v; deadline did not bound the call", elapsed)
	}
}

// TestRunNetwork_SucceedsAgainstLocalRemote verifies the bounded path
// still returns real output on a reachable remote.
func TestRunNetwork_SucceedsAgainstLocalRemote(t *testing.T) {
	upstream := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run(upstream, "init", "-q", "-b", "main")
	run(upstream, "commit", "-q", "--allow-empty", "-m", "init")

	clone := filepath.Join(t.TempDir(), "clone")
	cmd := exec.Command("git", "clone", "-q", upstream, clone)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %v: %s", err, out)
	}

	out, err := runNetwork(clone, "ls-remote", "--heads", "origin", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == "" {
		t.Fatal("expected ls-remote output for existing branch")
	}
}
