package docker

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Docker wraps docker command execution
type Docker struct {
	Runner CommandRunner
}

// CommandRunner interface for mocking
type CommandRunner interface {
	Run(cmd string, args ...string) (string, error)
	RunInDir(dir string, cmd string, args ...string) (string, error)
}

// RealRunner implements CommandRunner using os/exec
type RealRunner struct{}

// Run executes a command in the current directory
func (r *RealRunner) Run(cmd string, args ...string) (string, error) {
	return r.RunInDir("", cmd, args...)
}

// RunInDir executes a command in the specified directory.
// If dir is empty, runs in the current directory.
func (r *RealRunner) RunInDir(dir string, cmd string, args ...string) (string, error) {
	c := exec.Command(cmd, args...)
	if dir != "" {
		c.Dir = dir
	}
	c.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	err := c.Run()
	if err != nil {
		return stdout.String(), fmt.Errorf("docker command failed: %s %v: %s (stderr: %s)", cmd, args, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Option configures a Docker wrapper. Used by New for opt-in
// customization (e.g. tests injecting an xrr-backed CommandRunner).
type Option func(*Docker)

// WithRunner overrides the default RealRunner. Tests pass an
// xrr-backed runner to record/replay docker invocations
// deterministically — no real docker daemon needed in CI.
func WithRunner(r CommandRunner) Option {
	return func(d *Docker) { d.Runner = r }
}

// defaultOptions are applied by every New() call before any explicit
// options. Production leaves this empty; the binary's startup wires it
// from XRR_MODE/XRR_CASSETTE_DIR via SetDefaultOptions so every
// docker.New() call site gets an xrr-backed runner without threading
// Options through the codebase.
var defaultOptions []Option

// SetDefaultOptions installs Options that every subsequent New() call
// applies before any explicit options. Intended for one-shot startup
// wiring (xrr session injection) — not for concurrent reconfiguration.
// Tests that need an isolated default can call SetDefaultOptions(nil)
// in t.Cleanup.
func SetDefaultOptions(opts ...Option) {
	defaultOptions = opts
}

// New creates a new Docker wrapper. Without options it uses RealRunner —
// production behavior is unchanged. defaultOptions (set by
// SetDefaultOptions at startup) are applied first, then explicit opts.
func New(opts ...Option) *Docker {
	d := &Docker{Runner: &RealRunner{}}
	for _, opt := range defaultOptions {
		opt(d)
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// IsAvailable checks if docker is available
func (d *Docker) IsAvailable() bool {
	_, err := d.Runner.Run("docker", "version")
	return err == nil
}

// composeBaseArgs returns the leading docker compose arguments shared by all
// invocations: `compose -p <project>` when project is non-empty. Callers
// append the verb and verb-specific flags. Without `-p`, compose derives the
// project name from the cwd basename, which collides across hops that share
// branch names. See https://github.com/hop-top/git/issues/12.
func composeBaseArgs(project string) []string {
	args := []string{"compose"}
	if project != "" {
		args = append(args, "-p", project)
	}
	return args
}

// ComposeUp starts services. project namespaces compose so containers,
// networks, and volumes are hop-scoped. If overridePath is non-empty, it is
// used as an additional compose file via -f flags.
func (d *Docker) ComposeUp(dir, project string, detached bool, overridePath ...string) error {
	args := composeBaseArgs(project)

	// If override path provided, use explicit -f flags
	if len(overridePath) > 0 && overridePath[0] != "" {
		composeFile := FindComposeFile(dir)
		if composeFile != "" {
			args = append(args, "-f", composeFile)
			args = append(args, "-f", overridePath[0])
		}
	}

	// Explicitly load .env if it exists
	envFile := filepath.Join(dir, ".env")
	if _, err := os.Stat(envFile); err == nil {
		args = append(args, "--env-file", ".env")
	}

	args = append(args, "up")
	if detached {
		args = append(args, "-d")
	}
	_, err := d.Runner.RunInDir(dir, "docker", args...)
	return err
}

// ComposeStop stops services for the given project.
func (d *Docker) ComposeStop(dir, project string) error {
	args := append(composeBaseArgs(project), "stop")
	_, err := d.Runner.RunInDir(dir, "docker", args...)
	return err
}

// ComposeDown removes services for the given project.
func (d *Docker) ComposeDown(dir, project string) error {
	args := append(composeBaseArgs(project), "down")
	_, err := d.Runner.RunInDir(dir, "docker", args...)
	return err
}

// ComposePs lists containers for the given project.
func (d *Docker) ComposePs(dir, project string) (string, error) {
	args := append(composeBaseArgs(project), "ps", "--format", "json")
	return d.Runner.RunInDir(dir, "docker", args...)
}
