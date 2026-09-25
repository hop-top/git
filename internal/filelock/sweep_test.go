package filelock

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/spf13/afero"
)

var sweepTestName = regexp.MustCompile(`^data\.json\.[0-9]+\.tmp$`)

func isSweepTestName(name string) bool { return sweepTestName.MatchString(name) }

// sweepDir holds a stale and a fresh temp file, and stale files whose
// names the writer never gives its temp files.
type sweepDir struct {
	dir, lock, stale, fresh string
	others                  []string
}

func newSweepDir(t *testing.T) sweepDir {
	t.Helper()
	dir := t.TempDir()
	d := sweepDir{
		dir:   dir,
		lock:  filepath.Join(dir, "data.json.lock"),
		stale: filepath.Join(dir, "data.json.123456789.tmp"),
		fresh: filepath.Join(dir, "data.json.987654321.tmp"),
	}
	for _, name := range []string{
		"data.json",
		"data.json.tmp",
		"data.json.12ab.tmp",
		"data.json.123.tmp.bak",
		"other.json.123.tmp",
		"xdata.json.123.tmp",
	} {
		d.others = append(d.others, filepath.Join(dir, name))
	}
	old := time.Now().Add(-2 * StaleTempAge)
	for _, p := range append([]string{d.stale, d.fresh}, d.others...) {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if p != d.fresh {
			if err := os.Chtimes(p, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	// A directory named like a temp file is not one.
	if err := os.Mkdir(filepath.Join(dir, "data.json.555.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, "data.json.555.tmp"), old, old); err != nil {
		t.Fatal(err)
	}
	return d
}

func (d sweepDir) sweep(dryRun bool) ([]string, error) {
	g := Guard{Path: d.lock, Timeout: time.Minute}
	return g.SweepStale(afero.NewOsFs(), d.dir, isSweepTestName, time.Now().Add(-StaleTempAge), dryRun)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func (d sweepDir) assertUntouched(t *testing.T) {
	t.Helper()
	for _, p := range append([]string{d.fresh, filepath.Join(d.dir, "data.json.555.tmp")}, d.others...) {
		if !exists(p) {
			t.Errorf("%s was removed", filepath.Base(p))
		}
	}
	if exists(d.lock) {
		t.Errorf("lock file left behind")
	}
}

func TestSweepStale_RemovesStaleKeepsFresh(t *testing.T) {
	d := newSweepDir(t)

	removed, err := d.sweep(false)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(removed) != 1 || removed[0] != d.stale {
		t.Fatalf("removed %v, want [%s]", removed, d.stale)
	}
	if exists(d.stale) {
		t.Error("stale temp file kept")
	}
	d.assertUntouched(t)
}

func TestSweepStale_DryRunRemovesNothing(t *testing.T) {
	d := newSweepDir(t)

	would, err := d.sweep(true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if len(would) != 1 || would[0] != d.stale {
		t.Fatalf("would remove %v, want [%s]", would, d.stale)
	}
	if !exists(d.stale) {
		t.Error("dry run removed the stale temp file")
	}
	d.assertUntouched(t)
}

// A live writer holds the lock: the sweep removes nothing, dry run or
// not, and says the lock is busy.
func TestSweepStale_HeldLockSkips(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		d := newSweepDir(t)
		writer := New(d.lock)
		if ok, err := writer.TryAcquire(); !ok || err != nil {
			t.Fatalf("acquire: %v %v", ok, err)
		}

		removed, err := d.sweep(dryRun)
		if !errors.Is(err, ErrBusy) {
			t.Errorf("dryRun=%v: err = %v, want ErrBusy", dryRun, err)
		}
		if len(removed) != 0 {
			t.Errorf("dryRun=%v: removed %v under a held lock", dryRun, removed)
		}
		if !exists(d.stale) {
			t.Errorf("dryRun=%v: stale temp file removed under a held lock", dryRun)
		}
		_ = writer.Release()
	}
}

// Nothing to sweep takes no lock, so a directory that is gone is not
// recreated for the lock file.
func TestSweepStale_NothingStaleTakesNoLock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gone")
	g := Guard{Path: filepath.Join(dir, "data.json.lock")}
	removed, err := g.SweepStale(afero.NewOsFs(), dir, isSweepTestName, time.Now(), false)
	if err != nil || len(removed) != 0 {
		t.Fatalf("sweep of a missing dir: %v %v", removed, err)
	}
	if exists(dir) {
		t.Error("sweep created the missing directory")
	}
}
