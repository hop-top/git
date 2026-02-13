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

// New creates a new Docker wrapper
func New() *Docker {
	return &Docker{Runner: &RealRunner{}}
}

// IsAvailable checks if docker is available
func (d *Docker) IsAvailable() bool {
	_, err := d.Runner.Run("docker", "version")
	return err == nil
}

// ComposeUp starts services. If overridePath is non-empty, it is used as an
// additional compose file via -f flags.
func (d *Docker) ComposeUp(dir string, detached bool, overridePath ...string) error {
	args := []string{"compose"}

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

// ComposeStop stops services
func (d *Docker) ComposeStop(dir string) error {
	_, err := d.Runner.RunInDir(dir, "docker", "compose", "stop")
	return err
}

// ComposeDown removes services
func (d *Docker) ComposeDown(dir string) error {
	_, err := d.Runner.RunInDir(dir, "docker", "compose", "down")
	return err
}

// ComposePs lists containers
func (d *Docker) ComposePs(dir string) (string, error) {
	return d.Runner.RunInDir(dir, "docker", "compose", "ps", "--format", "json")
}
