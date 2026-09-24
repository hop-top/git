package events

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"hop.top/kit/go/runtime/bus"
)

func readLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("line %q is not JSON: %v", sc.Text(), err)
		}
		out = append(out, m)
	}
	return out
}

// A process that publishes and then calls os.Exit never reaches Close, so
// every Drain must leave its line on disk by itself. The sink is never
// closed in this test on purpose.
func TestFileSink_LineDurableWithoutClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "events.jsonl")
	s := NewFileSink(path, time.Second)

	ev := bus.NewEvent(WorktreeCreated, Source, WorktreeEvent{Path: "/w/a", Branch: "a"})
	if err := s.Drain(context.Background(), ev); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if err := s.Drain(context.Background(), bus.NewEvent(WorktreeRemoved, Source, WorktreeEvent{Branch: "a"})); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	lines := readLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if lines[0]["topic"] != string(WorktreeCreated) || lines[1]["topic"] != string(WorktreeRemoved) {
		t.Errorf("topics = %v, %v", lines[0]["topic"], lines[1]["topic"])
	}
	if lines[0]["source"] != Source {
		t.Errorf("source = %v, want %s", lines[0]["source"], Source)
	}
}

func TestFileSink_AppendsToExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte("{\"topic\":\"pre.existing.line.kept\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewFileSink(path, time.Second)
	if err := s.Drain(context.Background(), bus.NewEvent(WorktreeCreated, Source, WorktreeEvent{})); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if n := len(readLines(t, path)); n != 2 {
		t.Fatalf("got %d lines, want 2 (append, not truncate)", n)
	}
}

func TestFileSink_UnwritablePathReturnsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	s := NewFileSink(filepath.Join(dir, "sub", "events.jsonl"), time.Second)
	if err := s.Drain(context.Background(), bus.NewEvent(WorktreeCreated, Source, WorktreeEvent{})); err == nil {
		t.Fatal("Drain into unwritable dir returned nil error")
	}
}

// A FIFO with no reader blocks open(2) for writing indefinitely: the
// stand-in for a hung network mount. Drain must give up at the timeout
// instead of stalling the user's command.
func TestFileSink_BoundedByTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mkfifo")
	}
	path := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	// Unblock the abandoned writer goroutine at test end.
	t.Cleanup(func() {
		if f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = f.Close()
		}
	})

	s := NewFileSink(path, 50*time.Millisecond)
	start := time.Now()
	err := s.Drain(context.Background(), bus.NewEvent(WorktreeCreated, Source, WorktreeEvent{}))
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Drain err = %v, want timeout", err)
	}
	if elapsed > time.Second {
		t.Fatalf("Drain took %s, want ~50ms", elapsed)
	}
}

// Wire keys follow kit's bus contract: lowercase, snake_case. Cross-process
// consumers parse these names; capitalized Go field names would break them.
func TestPayloadJSONKeys(t *testing.T) {
	cases := []struct {
		name    string
		payload any
		want    []string
	}{
		{"worktree", WorktreeEvent{Path: "p", Branch: "b", HopspacePath: "h", RepoPath: "r"},
			[]string{"path", "branch", "hopspace_path", "repo_path"}},
		{"env", EnvEvent{Action: "start", Root: "r", Branch: "b"},
			[]string{"action", "root", "branch"}},
		{"hopspace", HopspaceEvent{Path: "p", Org: "o", Repo: "r"},
			[]string{"path", "org", "repo"}},
		{"deps", DepsEvent{WorktreePath: "w", Branch: "b"},
			[]string{"worktree_path", "branch"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.payload)
			if err != nil {
				t.Fatal(err)
			}
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			if len(m) != len(tc.want) {
				t.Errorf("keys = %v, want exactly %v", m, tc.want)
			}
			for _, k := range tc.want {
				if _, ok := m[k]; !ok {
					t.Errorf("missing key %q in %s", k, raw)
				}
			}
		})
	}
}
