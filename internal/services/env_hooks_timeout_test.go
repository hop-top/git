package services

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// lateWriter records writes and counts those that arrive after the call
// it was handed to has returned.
type lateWriter struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	returned bool
	late     int
}

func (w *lateWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.returned {
		w.late++
	}
	return w.buf.Write(p)
}

func (w *lateWriter) markReturned() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.returned = true
}

func (w *lateWriter) lateWrites() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.late
}

// A hook that overruns its deadline is killed with everything it started,
// and its output is collected before the call returns: under -q the
// output writer is a buffer the caller prints on failure, so a write that
// lands after return races the caller reading it.
func TestExecuteHooksWithTimeout_KillsHookAndStopsWriting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell hook")
	}
	dir := t.TempDir()
	// The hook keeps writing past the deadline, and so does a background
	// child it starts; each leaves a marker file if it outlives the kill.
	// Both finish on their own (~1s) so a regression leaks nothing.
	script := `#!/bin/sh
(sleep 1; echo child; touch "$HOP_WORKTREE_PATH/child-survived") &
for i in 1 2 3 4 5 6; do echo tick; sleep 0.1; done
touch "$HOP_WORKTREE_PATH/hook-survived"
echo done
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "slow.sh"), []byte(script), 0755))

	out := &lateWriter{}
	start := time.Now()
	err := ExecuteHooksWithTimeout([]string{"slow.sh"}, HookContext{
		WorktreePath: dir,
		Command:      "start",
		Out:          out,
	}, 200*time.Millisecond)
	out.markReturned()
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Contains(t, err.Error(), "timed out")
	require.Less(t, elapsed, 5*time.Second, "timeout did not unblock the caller promptly")

	// Outlast the hook and its child.
	time.Sleep(2 * time.Second)

	if n := out.lateWrites(); n != 0 {
		t.Errorf("%d write(s) reached the output writer after ExecuteHooksWithTimeout returned", n)
	}
	for _, marker := range []string{"hook-survived", "child-survived"} {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			t.Errorf("%s exists: the timed-out hook's process group was not killed", marker)
		}
	}
}

// The -q case as it happens: the output writer is a plain buffer that the
// caller reads once the call returns. Run with -race; any write the hook
// makes after return is a data race on the buffer.
func TestExecuteHooksWithTimeout_BufferSafeAfterReturn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell hook")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nfor i in 1 2 3 4 5 6; do echo tick; sleep 0.1; done\necho late\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "slow.sh"), []byte(script), 0755))

	var buf bytes.Buffer
	err := ExecuteHooksWithTimeout([]string{"slow.sh"}, HookContext{
		WorktreePath: dir,
		Command:      "start",
		Out:          &buf,
	}, 200*time.Millisecond)
	require.Error(t, err)
	before := buf.String()

	time.Sleep(1 * time.Second)
	require.Equal(t, before, buf.String(), "the hook wrote to the output buffer after return")
}
