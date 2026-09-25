package git

import "os/exec"

// ConfigureProcessGroup arranges for c's cancellation to kill c and every
// process it starts, not just c itself, so a timed-out command cannot
// leave a descendant running and writing to its output. Call it before
// Start, and call the returned hook immediately after Start. What each
// platform guarantees is documented on configureProcessGroup in
// network_unix.go (process group, SIGKILL) and network_windows.go (job
// object, best-effort; pair it with Cmd.WaitDelay).
func ConfigureProcessGroup(c *exec.Cmd) func() {
	return configureProcessGroup(c)
}
