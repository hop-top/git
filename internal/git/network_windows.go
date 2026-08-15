//go:build windows

package git

import (
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// configureProcessGroup makes the command the root of a new process
// group and arranges for cancellation to terminate its whole tree via a
// job object. The returned hook must be called immediately after Start.
//
// Windows has no kill(-pgid, SIGKILL). A job object is the closest real
// equivalent: every process a job member creates joins the job too, and
// TerminateJobObject kills all members at once. Breakaway is impossible
// unless a child is spawned with CREATE_BREAKAWAY_FROM_JOB *and* the job
// sets JOB_OBJECT_LIMIT_BREAKAWAY_OK — we set neither. So the job, not
// the process group, is what actually reaps the transport helper (ssh,
// git-remote-https) that would otherwise hold the output pipes open.
//
// CREATE_NEW_PROCESS_GROUP is still set so console CTRL+C aimed at our
// own group does not reach the child. We deliberately do NOT cancel via
// GenerateConsoleCtrlEvent: CTRL_BREAK_EVENT is ignorable through
// SetConsoleCtrlHandler and Git for Windows installs such a handler
// precisely so it is not killed while waiting on children; the event
// reaches only processes sharing our console, excluding a transport that
// allocated its own; a zero group id would signal git-hop itself; and
// with no console attached the call just fails. It would compile and
// look correct without fixing the hang it exists for.
//
// Residual race, stated honestly: the job is created and limited before
// Start, but assignment needs a pid and so happens just after it. A
// grandchild spawned in the window between CreateProcess returning and
// AssignProcessToJobObject would escape the job. In practice git.exe
// parses arguments and reads config before forking any transport, so the
// window is effectively never hit — but it is not nil. Closing it fully
// needs CREATE_SUSPENDED plus ResumeThread, which os/exec does not
// expose. If the job cannot be created or assigned at all, cancellation
// degrades to killing the direct child only.
//
// In every degraded case Cmd.WaitDelay in runNetwork is the backstop: it
// force-closes the inherited pipes so Wait returns even with a live
// transport. Unblocking the caller is what the deadline promises; on
// Windows the tree kill is very likely but, unlike unix, not guaranteed.
func configureProcessGroup(c *exec.Cmd) func() {
	c.SysProcAttr = &windows.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP,
	}

	// Create the job and apply its limit up front so a setup failure
	// cannot be mistaken for a successful kill later.
	job, err := newKillOnCloseJob()
	if err != nil {
		// No job available: fall back to killing the direct child. The
		// grandchild may survive, and WaitDelay bounds the wait.
		c.Cancel = func() error {
			if c.Process == nil {
				return nil
			}
			return c.Process.Kill()
		}
		return func() {}
	}

	assigned := false
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		if assigned {
			// Kills git and every descendant that joined the job.
			return windows.TerminateJobObject(job, 1)
		}
		return c.Process.Kill()
	}

	return func() {
		if c.Process == nil {
			windows.CloseHandle(job) //nolint:errcheck // best-effort cleanup
			return
		}
		// Go exposes only the pid, not the underlying handle, so reopen
		// the process for the rights the job APIs require.
		h, err := windows.OpenProcess(
			windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
			false, uint32(c.Process.Pid))
		if err != nil {
			return
		}
		defer windows.CloseHandle(h) //nolint:errcheck // best-effort cleanup

		if err := windows.AssignProcessToJobObject(job, h); err == nil {
			assigned = true
		}
	}
}

// newKillOnCloseJob returns a job object whose members are killed when
// the last handle to it closes, so a stalled transport cannot outlive
// git-hop even if cancellation never runs.
func newKillOnCloseJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(job) //nolint:errcheck // best-effort cleanup
		return 0, err
	}
	return job, nil
}
