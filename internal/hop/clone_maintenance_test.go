package hop

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/git"
)

// The fetch a clone runs right before `git worktree add` must not start
// git's auto-maintenance. That run detaches, and since git 2.54 it
// includes worktree-prune: landing between add creating worktrees/<name>
// and writing its lock, it deletes the fresh entry and the add dies with
// "could not open 'worktrees/main/locked' for writing".
//
// The race itself cannot be timed from here, so this asserts on its
// precondition: no maintenance child anywhere in git's own trace of the
// clone.
func TestCloneBareRepo_FetchStartsNoAutoMaintenance(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main", src},
		{"-C", src, "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	trace := filepath.Join(dir, "trace2.json")
	t.Setenv("GIT_TRACE2_EVENT", trace)

	hub := filepath.Join(dir, "hub")
	require.NoError(t, cloneBareRepo(afero.NewOsFs(), git.New(), src, hub, "main"))

	var sawFetch bool
	for _, ev := range readTrace2(t, trace) {
		if ev.Event == "cmd_name" && ev.Name == "fetch" {
			sawFetch = true
		}
		if ev.Event == "child_start" && (slices.Contains(ev.Argv, "maintenance") ||
			(slices.Contains(ev.Argv, "gc") && slices.Contains(ev.Argv, "--auto"))) {
			t.Errorf("clone started git housekeeping against the hub it is setting up: %v", ev.Argv)
		}
	}
	require.True(t, sawFetch, "trace2 recorded no fetch; the assertion above checked nothing")
}

type trace2Event struct {
	Event string   `json:"event"`
	Name  string   `json:"name"`
	Argv  []string `json:"argv"`
}

func readTrace2(t *testing.T, path string) []trace2Event {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	var events []trace2Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var ev trace2Event
		if json.Unmarshal(sc.Bytes(), &ev) == nil {
			events = append(events, ev)
		}
	}
	require.NoError(t, sc.Err())
	return events
}
