package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// DefaultNetworkTimeout bounds git subcommands that contact a remote
// (ls-remote, push --delete). Git itself applies no wall-clock deadline
// to these, so an unreachable host, a black-holed TCP connect, or a
// stalled TLS/SSH handshake blocks forever. Every hop command that
// reaches the network must fail fast with a stated cause instead.
const DefaultNetworkTimeout = 10 * time.Second

// networkTimeoutConfigKey is the git config key that overrides
// DefaultNetworkTimeout, in seconds. A value of 0 disables the deadline.
const networkTimeoutConfigKey = "hop.remote.timeout"

// ErrNetworkTimeout reports that a remote-touching git command exceeded
// its deadline. Callers surface this rather than hanging.
var ErrNetworkTimeout = errors.New("git remote operation timed out")

// networkTimeout resolves the deadline for remote-touching commands.
// Order: git config hop.remote.timeout (seconds), else the default.
// Non-numeric or negative values fall back to the default; 0 disables
// the deadline for users who genuinely want to wait.
//
// dir scopes the lookup so a per-repo override is honored, matching how
// every other hop.* key resolves.
func networkTimeout(dir string) time.Duration {
	args := []string{}
	if dir != "" {
		args = append(args, "-C", dir)
	}
	args = append(args, "config", "--get", networkTimeoutConfigKey)

	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return DefaultNetworkTimeout
	}
	secs, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || secs < 0 {
		return DefaultNetworkTimeout
	}
	return time.Duration(secs) * time.Second
}

// runNetwork executes a remote-touching git command under a deadline.
//
// It bypasses CommandRunner deliberately: the deadline must wrap the
// real process so it can be killed. Mocked runners never reach the
// network, so they never need it.
func runNetwork(dir string, args ...string) (string, error) {
	timeout := networkTimeout(dir)

	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	c := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		c.Dir = dir
	}
	// git delegates the transport to a helper (ssh, git-remote-https).
	// Killing only the direct child leaves that grandchild holding the
	// output pipes, and Cmd.Run blocks on them until it exits on its
	// own — the deadline would expire without unblocking the caller.
	// Run the command in its own process group and signal the whole
	// group so the transport dies with it.
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		// Negative pid targets the process group.
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
	// Belt and braces: if a transport somehow survives the group kill,
	// stop waiting on the inherited pipes shortly after.
	c.WaitDelay = 2 * time.Second
	c.Env = os.Environ()
	// Never block on an interactive credential prompt: a prompt on a
	// non-tty is an indefinite stall.
	c.Env = append(c.Env, "GIT_TERMINAL_PROMPT=0")
	// Same for ssh's passphrase and host-key prompts. Respect a
	// user-configured GIT_SSH_COMMAND rather than clobbering it; the
	// deadline still bounds the call either way.
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		c.Env = append(c.Env, "GIT_SSH_COMMAND=ssh -oBatchMode=yes")
	}

	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	err := c.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("%w after %s: git %s; retry with a reachable remote or raise 'git config %s'",
			ErrNetworkTimeout, timeout, strings.Join(args, " "), networkTimeoutConfigKey)
	}
	if err != nil {
		return stdout.String(), fmt.Errorf("git command failed: git %v: %s (stderr: %s)",
			args, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}
