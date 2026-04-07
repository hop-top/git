package xrrx

import (
	"hop.top/git/internal/docker"
	"hop.top/git/internal/git"
)

// InstallFromEnv reads XRR_MODE/XRR_CASSETTE_DIR and, when xrr is
// enabled, installs xrr-backed default Options on internal/git and
// internal/docker so every git.New() and docker.New() call in the
// running binary picks up the session.
//
// Returns nil when xrr is not enabled (production default). Returns a
// non-nil error when the env vars are inconsistent — callers should
// fail loudly so a misconfigured test harness does not silently fall
// back to live calls.
//
// Intended to be called once at binary startup, before any cobra
// command runs. Calling it multiple times is safe but only the most
// recent call's options remain active.
func InstallFromEnv() error {
	sess, err := SessionFromEnv()
	if err != nil {
		return err
	}
	if sess == nil {
		return nil // production: leave defaults empty
	}

	gitRunner := NewRunner(&git.RealRunner{}, sess)
	dockerRunner := NewRunner(&docker.RealRunner{}, sess)

	git.SetDefaultOptions(git.WithRunner(gitRunner))
	docker.SetDefaultOptions(docker.WithRunner(dockerRunner))

	return nil
}
