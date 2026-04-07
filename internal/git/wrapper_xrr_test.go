package git_test

import (
	"testing"

	"hop.top/git/internal/git"
	"hop.top/git/internal/xrrx"
	xrr "hop.top/xrr"
)

// fakeRunner is a stub git.CommandRunner whose responses tests can
// pre-program. The xrr session sees its output, records it, then on
// replay must reproduce it without invoking fakeRunner again.
type fakeRunner struct {
	calls  int
	stdout string
}

func (f *fakeRunner) Run(cmd string, args ...string) (string, error) {
	return f.RunInDir("", cmd, args...)
}

func (f *fakeRunner) RunInDir(dir, cmd string, args ...string) (string, error) {
	f.calls++
	return f.stdout, nil
}

// TestNew_WithRunner_RecordReplay proves the WithRunner option lets a
// test inject an xrr-backed CommandRunner that records git invocations
// in one session and replays them in another, without touching the real
// git binary on replay.
func TestNew_WithRunner_RecordReplay(t *testing.T) {
	dir := t.TempDir()
	headSha := "deadbeefcafebabe1234567890abcdef12345678\n"

	// --- record: stub returns the SHA, xrr writes it to the cassette.
	recReal := &fakeRunner{stdout: headSha}
	recRunner := xrrx.NewRunner(recReal,
		xrr.NewSession(xrr.ModeRecord, xrr.NewFileCassette(dir)))
	g := git.New(git.WithRunner(recRunner))

	out, err := g.Runner.Run("git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if out != headSha {
		t.Fatalf("record stdout = %q, want %q", out, headSha)
	}
	if recReal.calls != 1 {
		t.Fatalf("real runner called %d times during record, want 1", recReal.calls)
	}

	// --- replay: a fresh stub that MUST NOT be called.
	replReal := &fakeRunner{stdout: "must not be observed"}
	replRunner := xrrx.NewRunner(replReal,
		xrr.NewSession(xrr.ModeReplay, xrr.NewFileCassette(dir)))
	g2 := git.New(git.WithRunner(replRunner))

	out2, err := g2.Runner.Run("git", "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if out2 != headSha {
		t.Fatalf("replay stdout = %q, want %q", out2, headSha)
	}
	if replReal.calls != 0 {
		t.Fatalf("real runner called %d times during replay, want 0", replReal.calls)
	}
}

// TestNew_DefaultRunner asserts the zero-option New() keeps the
// production RealRunner — no behavioral change for existing callers.
func TestNew_DefaultRunner(t *testing.T) {
	g := git.New()
	if _, ok := g.Runner.(*git.RealRunner); !ok {
		t.Fatalf("default Runner = %T, want *git.RealRunner", g.Runner)
	}
}

// TestNew_WithRunner_Replaces asserts the option actually replaces the
// default. Guards against future refactors that might accidentally
// set Runner after applying options.
func TestNew_WithRunner_Replaces(t *testing.T) {
	custom := &fakeRunner{}
	g := git.New(git.WithRunner(custom))
	if g.Runner != custom {
		t.Fatalf("Runner not replaced by WithRunner option")
	}
}

