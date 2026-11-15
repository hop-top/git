package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initRepoWithBranches(t *testing.T, branches ...string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.x", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.x")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "init")
	for _, b := range branches {
		run("branch", b)
	}
	return dir
}

func TestBranchExists(t *testing.T) {
	dir := initRepoWithBranches(t, "feat/a")
	g := New()

	if ok, err := g.BranchExists(dir, "feat/a"); err != nil || !ok {
		t.Errorf("existing branch = (%v, %v), want (true, nil)", ok, err)
	}
	if ok, err := g.BranchExists(dir, "feat/b"); err != nil || ok {
		t.Errorf("absent branch = (%v, %v), want (false, nil)", ok, err)
	}
	// A path prefix of an existing branch is not that branch.
	if ok, err := g.BranchExists(dir, "feat"); err != nil || ok {
		t.Errorf("prefix of a branch = (%v, %v), want (false, nil)", ok, err)
	}
}

// A probe that cannot run is an error, not "absent".
func TestBranchExists_ProbeFailureIsAnError(t *testing.T) {
	g := New()
	if ok, err := g.BranchExists(filepath.Join(t.TempDir(), "missing"), "feat/a"); err == nil {
		t.Errorf("probe in a missing dir = (%v, nil), want an error", ok)
	}
	notRepo := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(notRepo))
	if ok, err := g.BranchExists(notRepo, "feat/a"); err == nil {
		t.Errorf("probe outside a repository = (%v, nil), want an error", ok)
	}
	if g.LocalBranchExists(notRepo, "feat/a") {
		t.Errorf("LocalBranchExists reported a branch it could not probe")
	}
}
