package xrrx_test

import (
	"errors"
	"os/exec"
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

// TestRunner_NonZeroExitRecordedAndReplayed exercises xrr v0.1.0-alpha.2's
// new behavior: a clean process exit with non-zero code is persisted into
// the cassette envelope so replay can re-emit the same error. We use a
// real `false` subprocess via os/exec to produce a genuine
// *os/exec.ExitError; an errors.New stand-in would still record but the
// real-binary path is the integration we care about.
//
// xrrx.Runner returns (string, error) — on a non-zero exit, both record
// and replay must surface a non-nil error so callers (git.CommandRunner,
// docker.CommandRunner) see the same shape they would in production.
func TestRunner_NonZeroExitRecordedAndReplayed(t *testing.T) {
	if _, err := exec.LookPath("false"); err != nil {
		t.Skip("`false` binary unavailable on this platform")
	}

	dir := t.TempDir()

	// --- record: real `false` returns exit 1; runner surfaces the error.
	recSess := xrr.NewSession(xrr.ModeRecord, xrr.NewFileCassette(dir))
	rec := xrrx.NewRunner(&osExecReal{}, recSess)
	_, recErr := rec.Run("false")
	if recErr == nil {
		t.Fatal("record: expected non-nil error from `false`, got nil")
	}

	// --- replay: must re-emit a non-nil error matching the recorded exit.
	replSess := xrr.NewSession(xrr.ModeReplay, xrr.NewFileCassette(dir))
	repl := xrrx.NewRunner(&fakeReal{err: errors.New("must not be called")}, replSess)
	_, replErr := repl.Run("false")
	if replErr == nil {
		t.Fatal("replay: expected non-nil error for recorded non-zero exit, got nil")
	}
}

// osExecReal is a Real runner backed by real os/exec, used only by the
// non-zero-exit test where we need a genuine *exec.ExitError.
type osExecReal struct{}

func (o *osExecReal) Run(cmd string, args ...string) (string, error) {
	return o.RunInDir("", cmd, args...)
}

func (o *osExecReal) RunInDir(dir, cmd string, args ...string) (string, error) {
	c := exec.Command(cmd, args...)
	if dir != "" {
		c.Dir = dir
	}
	out, err := c.Output()
	return string(out), err
}

// dirAwareReal is a Real runner that returns per-directory responses,
// used by TestRunner_CwdInFingerprint to verify the same command run in
// different cwds yields distinct cassettes.
type dirAwareReal struct {
	calls    int
	perDir   map[string]string
	fallback string
}

func (d *dirAwareReal) Run(cmd string, args ...string) (string, error) {
	return d.RunInDir("", cmd, args...)
}

func (d *dirAwareReal) RunInDir(dir, cmd string, args ...string) (string, error) {
	d.calls++
	if v, ok := d.perDir[dir]; ok {
		return v, nil
	}
	return d.fallback, nil
}

// TestRunner_CwdInFingerprint exercises xrr v0.1.0-alpha.3's Cwd field:
// the same command run in two different directories must produce two
// distinct cassettes during record, and replay must return the right
// one for each directory. Before alpha.3, both calls collided on the
// same fingerprint and the second record overwrote the first.
func TestRunner_CwdInFingerprint(t *testing.T) {
	cassDir := t.TempDir()

	const (
		dirA = "/tmp/worktree-a"
		dirB = "/tmp/worktree-b"
	)
	real := &dirAwareReal{
		perDir: map[string]string{
			dirA: "sha-from-a\n",
			dirB: "sha-from-b\n",
		},
	}

	// --- record: same argv in two different cwds. Cassettes must NOT
	// collide; both invocations must be persisted.
	recSess := xrr.NewSession(xrr.ModeRecord, xrr.NewFileCassette(cassDir))
	rec := xrrx.NewRunner(real, recSess)

	outA, err := rec.RunInDir(dirA, "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("record dirA: %v", err)
	}
	if outA != "sha-from-a\n" {
		t.Fatalf("record dirA stdout = %q, want %q", outA, "sha-from-a\n")
	}

	outB, err := rec.RunInDir(dirB, "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("record dirB: %v", err)
	}
	if outB != "sha-from-b\n" {
		t.Fatalf("record dirB stdout = %q, want %q", outB, "sha-from-b\n")
	}

	if real.calls != 2 {
		t.Fatalf("real runner called %d times, want 2", real.calls)
	}

	// --- replay: a fresh fakeReal that MUST NOT be invoked. Both cwds
	// should resolve to their distinct recorded responses.
	notCalled := &dirAwareReal{fallback: "must not be observed"}
	replSess := xrr.NewSession(xrr.ModeReplay, xrr.NewFileCassette(cassDir))
	repl := xrrx.NewRunner(notCalled, replSess)

	replA, err := repl.RunInDir(dirA, "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("replay dirA: %v", err)
	}
	if replA != "sha-from-a\n" {
		t.Fatalf("replay dirA stdout = %q, want %q (collision: cwd not in fingerprint?)", replA, "sha-from-a\n")
	}

	replB, err := repl.RunInDir(dirB, "git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("replay dirB: %v", err)
	}
	if replB != "sha-from-b\n" {
		t.Fatalf("replay dirB stdout = %q, want %q (collision: cwd not in fingerprint?)", replB, "sha-from-b\n")
	}

	if notCalled.calls != 0 {
		t.Fatalf("real runner called %d times during replay, want 0", notCalled.calls)
	}
}
