package filelock

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
)

// A Guard naming no Busy error gives up with ErrBusy.
func TestGuard_DefaultBusyError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	holder := New(path)
	if ok, err := holder.TryAcquire(); err != nil || !ok {
		t.Fatalf("TryAcquire: ok=%v err=%v", ok, err)
	}
	defer holder.Release()

	err := Guard{Path: path, Timeout: 20 * time.Millisecond}.Do(afero.NewOsFs(), func() error {
		t.Fatal("fn ran without the lock")
		return nil
	})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
}

// Off the real disk, two runs on one lock path still take turns.
func TestGuard_MemoryFsSerialises(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	fs := afero.NewMemMapFs()
	g := Guard{Path: path, Timeout: time.Second}

	inside := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- g.Do(fs, func() error {
			close(inside)
			<-release
			return nil
		})
	}()
	<-inside

	entered := make(chan struct{})
	go func() {
		_ = g.Do(fs, func() error { close(entered); return nil })
	}()
	select {
	case <-entered:
		t.Fatal("second run entered while the first held the lock")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("second run never entered")
	}
}
