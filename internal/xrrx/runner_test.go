package xrrx_test

import (
	"errors"
	"testing"

	"hop.top/git/internal/xrrx"
	xrr "hop.top/xrr"
)

// fakeReal is an in-memory Real runner. It records every call so the
// test can assert the runner is invoked exactly once during record and
// not at all during replay.
type fakeReal struct {
	calls    int
	stdout   string
	err      error
	lastDir  string
	lastCmd  string
	lastArgs []string
}

func (f *fakeReal) Run(cmd string, args ...string) (string, error) {
	return f.RunInDir("", cmd, args...)
}

func (f *fakeReal) RunInDir(dir, cmd string, args ...string) (string, error) {
	f.calls++
	f.lastDir, f.lastCmd, f.lastArgs = dir, cmd, args
	return f.stdout, f.err
}

func TestRunner_RecordReplayRoundTrip(t *testing.T) {
	dir := t.TempDir()
	real := &fakeReal{stdout: "abc1234\n"}

	// --- record
	recSess := xrr.NewSession(xrr.ModeRecord, xrr.NewFileCassette(dir))
	rec := xrrx.NewRunner(real, recSess)
	out, err := rec.RunInDir("/some/path", "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("record run: %v", err)
	}
	if out != "abc1234\n" {
		t.Fatalf("record stdout = %q, want %q", out, "abc1234\n")
	}
	if real.calls != 1 {
		t.Fatalf("real runner called %d times during record, want 1", real.calls)
	}

	// --- replay (fresh fakeReal — must NOT be invoked)
	real2 := &fakeReal{err: errors.New("real runner must not be called in replay mode")}
	replSess := xrr.NewSession(xrr.ModeReplay, xrr.NewFileCassette(dir))
	repl := xrrx.NewRunner(real2, replSess)
	out2, err := repl.RunInDir("/some/path", "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("replay run: %v", err)
	}
	if out2 != "abc1234\n" {
		t.Fatalf("replay stdout = %q, want %q", out2, "abc1234\n")
	}
	if real2.calls != 0 {
		t.Fatalf("real runner called %d times during replay, want 0", real2.calls)
	}
}

func TestRunner_PassthroughDelegates(t *testing.T) {
	real := &fakeReal{stdout: "passthrough-stdout"}
	sess := xrr.NewSession(xrr.ModePassthrough, xrr.NewFileCassette(t.TempDir()))
	r := xrrx.NewRunner(real, sess)

	out, err := r.Run("docker", "version")
	if err != nil {
		t.Fatalf("passthrough run: %v", err)
	}
	if out != "passthrough-stdout" {
		t.Fatalf("stdout = %q, want %q", out, "passthrough-stdout")
	}
	if real.lastCmd != "docker" || len(real.lastArgs) != 1 || real.lastArgs[0] != "version" {
		t.Fatalf("real runner saw %q %v, want docker [version]", real.lastCmd, real.lastArgs)
	}
}

func TestRunner_ReplayMissReturnsCassetteMiss(t *testing.T) {
	sess := xrr.NewSession(xrr.ModeReplay, xrr.NewFileCassette(t.TempDir()))
	r := xrrx.NewRunner(&fakeReal{}, sess)

	_, err := r.Run("git", "status")
	if !errors.Is(err, xrr.ErrCassetteMiss) {
		t.Fatalf("expected ErrCassetteMiss, got %v", err)
	}
}
