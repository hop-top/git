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
func (r *Runner) RunInDir(dir, cmd string, args ...string) (string, error) {
	req := &xexec.Request{Argv: append([]string{cmd}, args...)}
	resp, err := r.sess.Record(context.Background(), r.adapter, req,
		func() (xrr.Response, error) {
			out, runErr := r.real.RunInDir(dir, cmd, args...)
			return &xexec.Response{
				Stdout:   out,
				ExitCode: exitCodeFromErr(runErr),
			}, runErr
		})
	if err != nil {
		return "", err
	}
	return stdoutOf(resp), nil
}

// stdoutOf extracts stdout from either a typed exec response (record /
// passthrough) or a RawResponse map (replay).
func stdoutOf(resp xrr.Response) string {
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

// exitCodeFromErr returns 0 on nil, 1 otherwise. The exec adapter does
// not currently expose process exit codes from os/exec wrappers, so this
// is best-effort. See xrr feedback: exec adapter exit-code propagation.
func exitCodeFromErr(err error) int {
	if err == nil {
		return 0
	}
	return 1
}
