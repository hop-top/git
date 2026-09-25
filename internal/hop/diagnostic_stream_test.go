package hop_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
)

// captureStreams returns what fn wrote to os.Stdout and os.Stderr.
func captureStreams(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	read := func(f **os.File) func() string {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		prev := *f
		*f = w
		done := make(chan string)
		go func() {
			var buf bytes.Buffer
			_, _ = io.Copy(&buf, r)
			done <- buf.String()
		}()
		return func() string {
			*f = prev
			_ = w.Close()
			return <-done
		}
	}
	stopOut := read(&os.Stdout)
	stopErr := read(&os.Stderr)
	fn()
	return stopOut(), stopErr()
}

func withHumanMode(t *testing.T) {
	t.Helper()
	prev := output.CurrentMode
	output.CurrentMode = output.ModeHuman
	t.Cleanup(func() { output.CurrentMode = prev })
}

// A rollback step that fails is reported as an error on stderr.
func TestTransactionRollback_ErrorGoesToStderr(t *testing.T) {
	withHumanMode(t)
	tx := hop.NewTransaction()
	tx.AddStep(hop.TransactionStep{
		Name:     "step",
		Execute:  func() error { return nil },
		Rollback: func() error { return errors.New("undo failed") },
	})
	if err := tx.Execute(); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := captureStreams(t, tx.Rollback)

	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if want := "error: rollback failed: undo failed\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}
