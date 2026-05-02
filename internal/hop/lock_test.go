package hop

import (
	"path/filepath"
	"testing"
)

func TestFileLock_AcquireRelease(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	l := NewFileLock(path)
	ok, err := l.TryAcquire()
	if err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	if !ok {
		t.Fatal("expected to acquire fresh lock")
	}

	if err := l.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
}

func TestFileLock_SecondAcquireFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	l1 := NewFileLock(path)
	ok, err := l1.TryAcquire()
	if err != nil || !ok {
		t.Fatalf("first acquire: ok=%v err=%v", ok, err)
	}
	defer l1.Release()

	l2 := NewFileLock(path)
	ok2, err := l2.TryAcquire()
	if err != nil {
		t.Fatalf("second TryAcquire returned error: %v", err)
	}
	if ok2 {
		t.Fatal("expected second TryAcquire to return false (held by l1)")
	}
}

func TestFileLock_ReleaseAndReacquire(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	l1 := NewFileLock(path)
	ok, _ := l1.TryAcquire()
	if !ok {
		t.Fatal("first acquire failed")
	}
	if err := l1.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	l2 := NewFileLock(path)
	ok2, err := l2.TryAcquire()
	if err != nil {
		t.Fatalf("reacquire: %v", err)
	}
	if !ok2 {
		t.Fatal("expected reacquire after release to succeed")
	}
	_ = l2.Release()
}

func TestFileLock_ReleaseUnacquired(t *testing.T) {
	l := NewFileLock("/tmp/never-acquired.lock")
	if err := l.Release(); err != nil {
		t.Errorf("Release on never-acquired lock returned error: %v", err)
	}
}

func TestFileLock_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "subdir", "test.lock")

	l := NewFileLock(path)
	ok, err := l.TryAcquire()
	if err != nil {
		t.Fatalf("TryAcquire with nested parent: %v", err)
	}
	if !ok {
		t.Fatal("expected acquire to succeed")
	}
	_ = l.Release()
}
