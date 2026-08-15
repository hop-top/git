//go:build !windows

package git

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup runs the command in its own process group and
// kills the whole group on cancellation.
//
// This is the strong guarantee: SIGKILL to the negated pid reaches every
// process in the group, so the transport helper (ssh, git-remote-https)
// dies with git rather than surviving to hold the inherited output pipes
// open past the deadline.
//
// Everything is set up before Start, so the returned post-start hook is
// a no-op here; it exists for Windows, where the kill can only be armed
// once the process has a pid.
func configureProcessGroup(c *exec.Cmd) func() {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		// Negative pid targets the process group.
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
	return func() {}
}
