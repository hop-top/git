package hop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
)

func shortHopJSONLockTimeout(t *testing.T) {
	t.Helper()
	prev := hopJSONLockTimeout
	hopJSONLockTimeout = 200 * time.Millisecond
	t.Cleanup(func() { hopJSONLockTimeout = prev })
}

// A live holder makes the next writer wait, and give up with
// ErrHopJSONLocked rather than wait forever.
func TestWithHopJSONLock_HeldLockTimesOut(t *testing.T) {
	shortHopJSONLockTimeout(t)
	dir := t.TempDir()
	holder := NewFileLock(filepath.Join(dir, HopJSONLockName))
	if ok, err := holder.TryAcquire(); err != nil || !ok {
		t.Fatalf("TryAcquire: ok=%v err=%v", ok, err)
	}
	defer holder.Release()

	ran := false
	err := WithHopJSONLock(afero.NewOsFs(), dir, func() error { ran = true; return nil })
	if !errors.Is(err, ErrHopJSONLocked) {
		t.Fatalf("err = %v, want ErrHopJSONLocked", err)
	}
	if ran {
		t.Fatal("fn ran without the lock")
	}
}

// A writer waiting on the lock gets it once the holder lets go.
func TestWithHopJSONLock_WaitsForHolder(t *testing.T) {
	dir := t.TempDir()
	holder := NewFileLock(filepath.Join(dir, HopJSONLockName))
	if ok, err := holder.TryAcquire(); err != nil || !ok {
		t.Fatalf("TryAcquire: ok=%v err=%v", ok, err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(released)
		_ = holder.Release()
	}()

	err := WithHopJSONLock(afero.NewOsFs(), dir, func() error {
		select {
		case <-released:
			return nil
		default:
			return errors.New("fn ran while the holder still held the lock")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A lock file a crashed run left behind is not held by anyone and does
// not block; the run that takes it over removes it when done.
func TestWithHopJSONLock_StaleLockFileDoesNotBlock(t *testing.T) {
	shortHopJSONLockTimeout(t)
	dir := t.TempDir()
	lockPath := filepath.Join(dir, HopJSONLockName)
	if err := os.WriteFile(lockPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	ran := false
	if err := WithHopJSONLock(afero.NewOsFs(), dir, func() error { ran = true; return nil }); err != nil {
		t.Fatalf("stale lock file blocked the write: %v", err)
	}
	if !ran {
		t.Fatal("fn did not run")
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock file left behind, stat err=%v", err)
	}
}

// fn's error comes back, and the lock is released either way.
func TestWithHopJSONLock_ReturnsFnErrorAndReleases(t *testing.T) {
	dir := t.TempDir()
	want := errors.New("boom")
	if err := WithHopJSONLock(afero.NewOsFs(), dir, func() error { return want }); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if Held(filepath.Join(dir, HopJSONLockName)) {
		t.Fatal("lock still held after fn returned")
	}
}

// Off the real disk (in-memory tests, a dry run's view) nothing is
// written to the real disk.
func TestWithHopJSONLock_MemoryFsLeavesRealDiskAlone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hub")
	if err := WithHopJSONLock(afero.NewMemMapFs(), dir, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("in-memory lock touched the real disk, stat err=%v", err)
	}
}
