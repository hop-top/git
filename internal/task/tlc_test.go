package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubTLC writes an executable shell script standing in for tlc and
// returns its path. The script logs its argv to <dir>/calls.
func stubTLC(t *testing.T, body string) (bin, calls string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "tlc")
	calls = filepath.Join(dir, "calls")
	script := "#!/bin/sh\necho \"$*\" >> '" + calls + "'\n" + body + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, calls
}

func TestTLCResolve_ParsesTaskShowJSON(t *testing.T) {
	bin, calls := stubTLC(t, `cat <<'EOF'
{"logs":[],"task":{"id":"task_x","seq":7,"title":"Login redirect loops","status":"TODO","tags":["type:bug","area:auth"]}}
EOF`)
	r := &TLC{Bin: bin, Timeout: 5 * time.Second}

	got, err := r.Resolve(context.Background(), "T-7")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "T-7" || got.Title != "Login redirect loops" {
		t.Errorf("Resolve = %+v", got)
	}
	if strings.Join(got.Tags, ",") != "type:bug,area:auth" {
		t.Errorf("Tags = %v", got.Tags)
	}
	logged, _ := os.ReadFile(calls)
	if strings.TrimSpace(string(logged)) != "task show T-7 --format json" {
		t.Errorf("tlc invoked as %q", logged)
	}
}

func TestTLCResolve_NotOnPath(t *testing.T) {
	r := &TLC{Bin: filepath.Join(t.TempDir(), "no-such-tlc"), Timeout: time.Second}
	_, err := r.Resolve(context.Background(), "T-1")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestTLCResolve_CommandFails(t *testing.T) {
	bin, _ := stubTLC(t, `echo "task not found: $3" >&2; exit 1`)
	r := &TLC{Bin: bin, Timeout: 5 * time.Second}
	_, err := r.Resolve(context.Background(), "T-404")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "task not found: T-404") {
		t.Errorf("err = %v, want tlc's stderr in it", err)
	}
}

func TestTLCResolve_BadJSON(t *testing.T) {
	bin, _ := stubTLC(t, `echo 'Task: T-1 (human text)'`)
	r := &TLC{Bin: bin, Timeout: 5 * time.Second}
	if _, err := r.Resolve(context.Background(), "T-1"); err == nil {
		t.Fatal("want error for non-JSON output")
	}
}

func TestTLCResolve_MissingTitle(t *testing.T) {
	bin, _ := stubTLC(t, `echo '{"task":{"tags":["type:feat"]}}'`)
	r := &TLC{Bin: bin, Timeout: 5 * time.Second}
	if _, err := r.Resolve(context.Background(), "T-1"); err == nil {
		t.Fatal("want error when the task has no title")
	}
}

func TestTLCResolve_Timeout(t *testing.T) {
	bin, _ := stubTLC(t, `sleep 5`)
	r := &TLC{Bin: bin, Timeout: 200 * time.Millisecond}
	start := time.Now()
	_, err := r.Resolve(context.Background(), "T-1")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want a timeout", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Resolve took %v, want it bounded by the timeout", elapsed)
	}
}
