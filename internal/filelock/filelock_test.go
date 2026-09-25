package filelock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLock_AcquireRelease(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	l := New(path)
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

func TestLock_SecondAcquireFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	l1 := New(path)
	ok, err := l1.TryAcquire()
	if err != nil || !ok {
		t.Fatalf("first acquire: ok=%v err=%v", ok, err)
	}
	defer l1.Release()

	l2 := New(path)
	ok2, err := l2.TryAcquire()
	if err != nil {
		t.Fatalf("second TryAcquire returned error: %v", err)
	}
	if ok2 {
		t.Fatal("expected second TryAcquire to return false (held by l1)")
	}
}

func TestLock_ReleaseAndReacquire(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")

	l1 := New(path)
	ok, _ := l1.TryAcquire()
	if !ok {
		t.Fatal("first acquire failed")
	}
	if err := l1.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	l2 := New(path)
	ok2, err := l2.TryAcquire()
	if err != nil {
		t.Fatalf("reacquire: %v", err)
	}
	if !ok2 {
		t.Fatal("expected reacquire after release to succeed")
	}
	_ = l2.Release()
}

func TestLock_ReleaseUnacquired(t *testing.T) {
	l := New("/tmp/never-acquired.lock")
	if err := l.Release(); err != nil {
		t.Errorf("Release on never-acquired lock returned error: %v", err)
	}
}

func TestLock_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "subdir", "test.lock")

	l := New(path)
	ok, err := l.TryAcquire()
	if err != nil {
		t.Fatalf("TryAcquire with nested parent: %v", err)
	}
	if !ok {
		t.Fatal("expected acquire to succeed")
	}
	_ = l.Release()
}

// TestLock_ReleaseRemovesFile pins the contract that a released lock
// leaves nothing on disk: the lock file is unlinked, not merely unlocked.
// A lingering 0-byte lock file is what other tools mistake for state.
func TestLock_ReleaseRemovesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "test.lock")

	l := New(path)
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

// TestLock_HeldReportsLiveLockOnly: Held probes an existing lock file
// without creating it or its parent directory, so a cleanup pass can ask
// "is this stale?" without leaving a footprint.
func TestLock_HeldReportsLiveLockOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "absent", "test.lock")

	if Held(path) {
		t.Fatal("Held on a missing file must be false")
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("Held must not create the parent dir, stat err=%v", err)
	}

	live := filepath.Join(dir, "live.lock")
	l := New(live)
	if ok, err := l.TryAcquire(); err != nil || !ok {
		t.Fatalf("TryAcquire: ok=%v err=%v", ok, err)
	}
	defer l.Release()
	if !Held(live) {
		t.Fatal("Held must report true while another handle holds the lock")
	}
}

// TestLock_ReleaseAfterMoveKeepsNewLock: a holder that renamed the lock
// file's directory must not unlink the file now at the old path, which is
// another process's lock taken since; unlinking it would let a third
// process take the lock while the second holds it.
func TestLock_ReleaseAfterMoveKeepsNewLock(t *testing.T) {
	root := t.TempDir()
	from, to := filepath.Join(root, "from"), filepath.Join(root, "to")
	path := filepath.Join(from, "test.lock")

	mover := New(path)
	if ok, err := mover.TryAcquire(); err != nil || !ok {
		t.Fatalf("mover acquire: ok=%v err=%v", ok, err)
	}
	if err := os.Rename(from, to); err != nil {
		t.Fatalf("rename: %v", err)
	}

	second := New(path)
	if ok, err := second.TryAcquire(); err != nil || !ok {
		t.Fatalf("second acquire at the old path: ok=%v err=%v", ok, err)
	}
	defer second.Release()

	if err := mover.Release(); err != nil {
		t.Fatalf("mover release: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the second holder's lock file must survive the mover's release: %v", err)
	}
	third := New(path)
	ok, err := third.TryAcquire()
	if err != nil {
		t.Fatalf("third TryAcquire: %v", err)
	}
	if ok {
		_ = third.Release()
		t.Fatal("third process took the lock while the second holds it")
	}
}
