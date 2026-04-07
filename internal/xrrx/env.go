package xrrx

import (
	"fmt"
	"os"
	"path/filepath"

	xrr "hop.top/xrr"
)

// Environment variables read by SessionFromEnv. The convention follows
// the XRR_CASSETTE_DIR proposal in hop-top/xrr T-0039: a parent test
// process exports these before exec'ing the git-hop binary, the binary
// builds a session from them at startup, and all internal git/docker
// invocations flow through that session.
const (
	// EnvMode selects the xrr mode. Valid values: "record", "replay",
	// "passthrough". Empty or "off" disables xrr — production default.
	EnvMode = "XRR_MODE"

	// EnvCassetteDir is the directory cassettes are written to (record)
	// or read from (replay). Required when EnvMode is set to a non-off
	// value.
	EnvCassetteDir = "XRR_CASSETTE_DIR"
)

// SessionFromEnv constructs an xrr.FileSession from the EnvMode and
// EnvCassetteDir environment variables. Returns (nil, nil) when
// EnvMode is unset or "off" — the production default.
//
// Returns a non-nil error when EnvMode is set to something invalid,
// when EnvMode is set but EnvCassetteDir is missing, or when
// EnvCassetteDir is not an absolute path. The absolute-path
// requirement guards the cross-process use case (parent test sets
// the env var, child binary builds the session): a relative path
// would resolve against the child's cwd, which is rarely what the
// parent intended and produces silently mis-located cassettes.
//
// The caller is expected to fail loudly on any error: a misconfigured
// test harness should not silently fall back to live calls or write
// cassettes to the wrong place.
func SessionFromEnv() (*xrr.FileSession, error) {
	mode := os.Getenv(EnvMode)
	if mode == "" || mode == "off" {
		return nil, nil
	}

	dir := os.Getenv(EnvCassetteDir)
	if dir == "" {
		return nil, fmt.Errorf("xrrx: %s=%q but %s is unset", EnvMode, mode, EnvCassetteDir)
	}
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("xrrx: %s=%q must be an absolute path", EnvCassetteDir, dir)
	}

	var xrrMode xrr.Mode
	switch mode {
	case "record":
		xrrMode = xrr.ModeRecord
	case "replay":
		xrrMode = xrr.ModeReplay
	case "passthrough":
		xrrMode = xrr.ModePassthrough
	default:
		return nil, fmt.Errorf("xrrx: invalid %s=%q (want record|replay|passthrough|off)", EnvMode, mode)
	}

	return xrr.NewSession(xrrMode, xrr.NewFileCassette(dir)), nil
}
