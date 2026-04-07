// Package xrrx provides an xrr-backed CommandRunner for git-hop's
// internal/git and internal/docker wrappers, enabling deterministic
// record/replay of subprocess interactions in tests.
//
// Both git.CommandRunner and docker.CommandRunner share the same shape:
//
//	Run(cmd string, args ...string) (string, error)
//	RunInDir(dir, cmd string, args ...string) (string, error)
//
// xrrx.Runner implements that shape and delegates to a real underlying
// runner, mediated by an xrr.FileSession + exec adapter. In ModeRecord
// the real runner is invoked and the (request, response) pair is written
// to the cassette dir. In ModeReplay the cassette is consulted and the
// real runner is never called. In ModePassthrough the session forwards
// directly to the real runner.
package xrrx

import (
	"context"

	xrr "hop.top/xrr"
	xexec "hop.top/xrr/adapters/exec"
)

// Real is the minimal interface xrrx.Runner needs from an underlying
// command runner. Both git.RealRunner and docker.RealRunner satisfy it
// without modification.
type Real interface {
	Run(cmd string, args ...string) (string, error)
	RunInDir(dir, cmd string, args ...string) (string, error)
}

// Runner wraps a Real runner with an xrr session. It satisfies both
// git.CommandRunner and docker.CommandRunner because they share the same
// signatures.
type Runner struct {
	real    Real
	sess    *xrr.FileSession
	adapter *xexec.Adapter
}

// NewRunner constructs a Runner. real is the production runner the
// session delegates to in record/passthrough modes. sess controls the
// mode and cassette location.
func NewRunner(real Real, sess *xrr.FileSession) *Runner {
	return &Runner{
		real:    real,
		sess:    sess,
		adapter: xexec.NewAdapter(),
	}
}

// Run executes cmd in the current directory.
func (r *Runner) Run(cmd string, args ...string) (string, error) {
	return r.RunInDir("", cmd, args...)
}

// RunInDir executes cmd in dir. The interaction is mediated by the
// xrr session: it may be recorded, replayed, or passed through.
//
// On a non-zero process exit (*os/exec.ExitError), the exit code is
// captured in the recorded Response so replay sees the same code, AND
// the original error is returned to the caller (and recorded by xrr's
// alpha.2 error-persistence path so replay can re-emit it). Callers
// MUST treat a non-nil error as failure even though the Response is
// also populated. Non-exit errors (binary missing, ctx cancel)
// propagate as-is and are not recordable.
//
// dir is recorded into the request fingerprint via xexec.Request.Cwd
// (xrr v0.1.0-alpha.3+) so the same command run from different
// worktrees produces distinct cassette keys. Without this, multi-cwd
// recordings — which are the norm for git-hop tests — would all
// collide on the same fingerprint and overwrite each other on record /
// reuse the wrong recording on replay.
func (r *Runner) RunInDir(dir, cmd string, args ...string) (string, error) {
	req := &xexec.Request{
		Argv: append([]string{cmd}, args...),
		Cwd:  dir,
	}
	resp, err := r.sess.Record(context.Background(), r.adapter, req,
		func() (xrr.Response, error) {
			out, runErr := r.real.RunInDir(dir, cmd, args...)
			return wrapExecResult(out, runErr)
		})
	return stdoutOf(resp), err
}

// wrapExecResult populates the Response with the exit code (via
// xexec.ExitCodeFromError) and, critically, returns runErr verbatim so
// xrr v0.1.0-alpha.2's session.record can persist it into the cassette
// envelope. On replay, session.replay re-emits the recorded error,
// preserving the (string, error) contract that git.CommandRunner and
// docker.CommandRunner expose to callers.
//
// This differs from xrr's examples/wrap_command_runner which absorbs
// ExitError into the Response and returns nil — that pattern fits
// callers that read resp.ExitCode directly. We project to (string,
// error) and need the error round-tripped so non-zero exits remain
// observable.
func wrapExecResult(out string, runErr error) (xrr.Response, error) {
	resp := &xexec.Response{
		Stdout:   out,
		ExitCode: xexec.ExitCodeFromError(runErr),
	}
	return resp, runErr
}

// stdoutOf extracts stdout from either a typed exec response (record /
// passthrough) or a RawResponse map (replay).
func stdoutOf(resp xrr.Response) string {
	if resp == nil {
		return ""
	}
	switch v := resp.(type) {
	case *xexec.Response:
		return v.Stdout
	case *xrr.RawResponse:
		if s, ok := v.Payload["stdout"].(string); ok {
			return s
		}
	}
	return ""
}
