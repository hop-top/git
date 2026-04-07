package docker_test

import (
	"testing"

	"hop.top/git/internal/docker"
	"hop.top/git/internal/xrrx"
	xrr "hop.top/xrr"
)

// fakeRunner stubs docker.CommandRunner so the test can pre-program
// what `docker version` (or any other command) returns. xrr records
// the result, then on replay must reproduce it without invoking
// fakeRunner again.
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

// TestNew_WithRunner_RecordReplay proves docker.WithRunner lets a test
// inject an xrr-backed CommandRunner that records docker invocations
// in one session and replays them in another, without touching the
// real docker daemon on replay. This is the seam that lets the
// docker e2e tests run cassette-only in CI.
func TestNew_WithRunner_RecordReplay(t *testing.T) {
	dir := t.TempDir()
	versionOut := "Docker version 24.0.7, build afdd53b\n"

	// --- record: stub returns the version, xrr writes it to the cassette.
	recReal := &fakeRunner{stdout: versionOut}
	recRunner := xrrx.NewRunner(recReal,
		xrr.NewSession(xrr.ModeRecord, xrr.NewFileCassette(dir)))
	d := docker.New(docker.WithRunner(recRunner))

	out, err := d.Runner.Run("docker", "version")
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if out != versionOut {
		t.Fatalf("record stdout = %q, want %q", out, versionOut)
	}
	if recReal.calls != 1 {
		t.Fatalf("real runner called %d times during record, want 1", recReal.calls)
	}

	// --- replay: a fresh stub that MUST NOT be called.
	replReal := &fakeRunner{stdout: "must not be observed"}
	replRunner := xrrx.NewRunner(replReal,
		xrr.NewSession(xrr.ModeReplay, xrr.NewFileCassette(dir)))
	d2 := docker.New(docker.WithRunner(replRunner))

	out2, err := d2.Runner.Run("docker", "version")
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if out2 != versionOut {
		t.Fatalf("replay stdout = %q, want %q", out2, versionOut)
	}
	if replReal.calls != 0 {
		t.Fatalf("real runner called %d times during replay, want 0", replReal.calls)
	}
}

// TestNew_DefaultRunner asserts the zero-option New() keeps the
// production RealRunner — no behavioral change for existing callers.
func TestNew_DefaultRunner(t *testing.T) {
	d := docker.New()
	if _, ok := d.Runner.(*docker.RealRunner); !ok {
		t.Fatalf("default Runner = %T, want *docker.RealRunner", d.Runner)
	}
}

// TestNew_WithRunner_Replaces asserts the option actually replaces the
// default. Guards against future refactors that might accidentally
// set Runner after applying options.
func TestNew_WithRunner_Replaces(t *testing.T) {
	custom := &fakeRunner{}
	d := docker.New(docker.WithRunner(custom))
	if d.Runner != custom {
		t.Fatalf("Runner not replaced by WithRunner option")
	}
}
