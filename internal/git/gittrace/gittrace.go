// Package gittrace lets tests see which processes git itself started,
// through git's trace2 event stream. Children git spawns on its own,
// such as the auto-maintenance run at the end of a fetch, never pass
// through git-hop's runner, so this is the only place they show up.
package gittrace

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Event is the part of a trace2 event the tests read.
type Event struct {
	Event string   `json:"event"`
	Name  string   `json:"name"`
	Argv  []string `json:"argv"`
}

// Trace is a trace2 event file every git process of the test appends to.
type Trace struct{ path string }

// Start points GIT_TRACE2_EVENT at a fresh file for the rest of the
// test. It sets process env, so the test must not be parallel.
func Start(t *testing.T) *Trace {
	t.Helper()
	path := filepath.Join(t.TempDir(), "trace2.json")
	t.Setenv("GIT_TRACE2_EVENT", path)
	return &Trace{path: path}
}

// Events returns every event recorded so far.
func (tr *Trace) Events(t *testing.T) []Event {
	t.Helper()
	f, err := os.Open(tr.path)
	if err != nil {
		t.Fatalf("read trace2 events: %v", err)
	}
	defer f.Close()

	var events []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var ev Event
		if json.Unmarshal(sc.Bytes(), &ev) == nil {
			events = append(events, ev)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read trace2 events: %v", err)
	}
	return events
}

// RequireNoAutoMaintenance fails the test if any git process started
// housekeeping (`git maintenance`, or `git gc --auto` on older git).
// It also fails when no fetch was traced at all, so a trace that
// recorded nothing cannot pass for a clean one.
func (tr *Trace) RequireNoAutoMaintenance(t *testing.T) {
	t.Helper()
	var sawFetch bool
	for _, ev := range tr.Events(t) {
		if ev.Event == "cmd_name" && ev.Name == "fetch" {
			sawFetch = true
		}
		if ev.Event == "child_start" && (slices.Contains(ev.Argv, "maintenance") ||
			(slices.Contains(ev.Argv, "gc") && slices.Contains(ev.Argv, "--auto"))) {
			t.Errorf("a fetch started git housekeeping: %v", ev.Argv)
		}
	}
	if !sawFetch {
		t.Fatal("trace2 recorded no fetch; nothing was checked")
	}
}
