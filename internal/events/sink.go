package events

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"hop.top/kit/go/runtime/bus"
)

// FileSink appends each event as one JSONL line to a file, using kit's
// JSONLSink encoding.
//
// kit's file-backed JSONLSink buffers until Close, and git-hop leaves
// through os.Exit on many paths (output.Fatal, the navigation-handled
// switch), which skips Close and would drop buffered lines. FileSink
// therefore opens, drains and closes a JSONLSink per event: every line is
// on disk when Drain returns, no descriptor outlives the call, and a line
// under the 4 KiB buffer lands in a single O_APPEND write, so concurrent
// git-hop processes sharing one file do not interleave.
//
// Drain is bounded by a timeout. A path on a hung mount (or a FIFO with no
// reader) blocks open(2) indefinitely; past the deadline Drain abandons the
// write and returns an error so the user's command is never held up.
type FileSink struct {
	path    string
	timeout time.Duration
}

var _ bus.Sink = (*FileSink)(nil)

// NewFileSink returns a FileSink appending to path, creating parent
// directories on first write. Each Drain waits at most timeout.
func NewFileSink(path string, timeout time.Duration) *FileSink {
	return &FileSink{path: path, timeout: timeout}
}

// Drain writes e as one JSONL line.
func (s *FileSink) Drain(ctx context.Context, e bus.Event) error {
	done := make(chan error, 1)
	go func() { done <- s.write(ctx, e) }()

	timer := time.NewTimer(s.timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		return fmt.Errorf("event sink %s: write timed out after %s", s.path, s.timeout)
	}
}

func (s *FileSink) write(ctx context.Context, e bus.Event) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("event sink %s: %w", s.path, err)
	}
	js, err := bus.NewJSONLSinkFile(s.path)
	if err != nil {
		return fmt.Errorf("event sink %s: %w", s.path, err)
	}
	if err := js.Drain(ctx, e); err != nil {
		_ = js.Close()
		return fmt.Errorf("event sink %s: %w", s.path, err)
	}
	if err := js.Close(); err != nil {
		return fmt.Errorf("event sink %s: %w", s.path, err)
	}
	return nil
}

// Close is a no-op: FileSink holds no open resources between events.
func (s *FileSink) Close() error { return nil }
