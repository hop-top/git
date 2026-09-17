package hop

import (
	"os"
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

// TestFileLock_ReleaseRemovesFile pins the contract that a released lock
// leaves nothing on disk: the lock file is unlinked, not merely unlocked.
// A lingering 0-byte lock file is what other tools mistake for state.
func TestFileLock_ReleaseRemovesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "test.lock")

	l := NewFileLock(path)
	ok, err := l.TryAcquire()
	if err != nil || !ok {
		t.Fatalf("TryAcquire: ok=%v err=%v", ok, err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected lock file removed after Release, stat err=%v", err)
	}
}

// TestFileLock_HeldReportsLiveLockOnly: Held probes an existing lock file
// without creating it or its parent directory, so a cleanup pass can ask
// "is this stale?" without leaving a footprint.
func TestFileLock_HeldReportsLiveLockOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "absent", "test.lock")

	if Held(path) {
		t.Fatal("Held on a missing file must be false")
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("Held must not create the parent dir, stat err=%v", err)
	}

	live := filepath.Join(dir, "live.lock")
	l := NewFileLock(live)
	if ok, err := l.TryAcquire(); err != nil || !ok {
		t.Fatalf("TryAcquire: ok=%v err=%v", ok, err)
	}
	defer l.Release()
	if !Held(live) {
		t.Fatal("Held must report true while another handle holds the lock")
	}
}
