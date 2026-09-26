package hooks

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"

	"hop.top/git/internal/filelock"
	"hop.top/git/internal/hop"
)

// A mirror resolves the hopspace hooks dir before it takes the hopspace's
// hop.json lock. doctor --fix may move that dir in between, under the same
// lock, after hop.dataLayout changed. Once the mirror holds the lock it
// must notice and write where the dir went, never recreate it at the old
// path. These run on disk: only there are the locks file locks.

const (
	movedRepoID = "github.com/acme/widgets"
	movedURI    = "https://github.com/acme/widgets.git"
)

// moveBeforeLock makes the next mirror's first lock wait meet a doctor
// --fix move: under the lock of the hopspace the mirror resolved, the
// layout switches to the default and from is renamed to to.
func moveBeforeLock(t *testing.T, fs afero.Fs, from, to string) {
	t.Helper()
	prev := beforeMirrorLock
	t.Cleanup(func() { beforeMirrorLock = prev })
	moved := false
	beforeMirrorLock = func(hooksDir string) {
		if moved {
			return
		}
		moved = true
		err := hop.WithHopJSONLock(fs, filepath.Dir(hooksDir), func() error {
			if out, err := exec.Command("git", "config", "--global", "hop.dataLayout", "{org}/{repo}").CombinedOutput(); err != nil {
				t.Fatalf("git config: %v\n%s", err, out)
			}
			if err := fs.MkdirAll(filepath.Dir(to), 0o755); err != nil {
				return err
			}
			return fs.Rename(from, to)
		})
		if err != nil {
			t.Fatalf("move %s: %v", from, err)
		}
	}
}

func mirrorMoved(t *testing.T, fs afero.Fs, wt string) Result {
	t.Helper()
	res, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: wt, RepoID: movedRepoID, RepoURI: movedURI, Mode: ModeCopy,
	})
	if err != nil {
		t.Fatalf("MirrorCommittedHooks: %v", err)
	}
	return res
}

func assertMirroredAfterMove(t *testing.T, fs afero.Fs, res Result, oldHooks, newHooks string) {
	t.Helper()
	if res.Installed != 1 {
		t.Fatalf("Installed = %d, want 1 (%+v)", res.Installed, res.Hooks)
	}
	if ok, _ := afero.Exists(fs, oldHooks); ok {
		t.Errorf("mirror recreated %s after the move", oldHooks)
	}
	body, err := afero.ReadFile(fs, filepath.Join(newHooks, "post-worktree-add"))
	if err != nil {
		t.Fatalf("hook not mirrored where the dir went: %v", err)
	}
	if string(body) != "#!/bin/sh\necho new\n" {
		t.Errorf("mirrored hook = %q", body)
	}
}

// The hooks dir alone moves, as doctor --fix moves one left where
// releases before hop.dataLayout mirrored it.
func TestMirror_HooksDirMovedWhileWaitingForLock(t *testing.T) {
	withGlobalGitConfig(t, map[string]string{"hop.dataLayout": "{host}/{org}/{repo}"})
	root := t.TempDir()
	data := filepath.Join(root, "data")
	withDataHome(t, data)
	fs := afero.NewOsFs()
	wt := filepath.Join(root, "wt")
	writeHook(t, fs, wt, "post-worktree-add", "#!/bin/sh\necho new\n", 0o755)

	oldHooks := filepath.Join(data, "github.com", "acme", "widgets", "hooks")
	newHooks := filepath.Join(data, "acme", "widgets", "hooks")
	writeFile(t, fs, filepath.Join(oldHooks, "pre-worktree-add"), "#!/bin/sh\necho old\n")
	// What the move leaves beside the hooks dir: the hopspace is not empty.
	writeFile(t, fs, filepath.Join(filepath.Dir(oldHooks), ".DS_Store"), "")
	moveBeforeLock(t, fs, oldHooks, newHooks)

	res := mirrorMoved(t, fs, wt)

	assertMirroredAfterMove(t, fs, res, oldHooks, newHooks)
	if ok, _ := afero.Exists(fs, filepath.Join(newHooks, "pre-worktree-add")); !ok {
		t.Error("the hook the dir already held must stay with it")
	}
}

// The whole hopspace moves, as doctor --fix moves one left at another
// hop.dataLayout path; it had no hooks dir yet.
func TestMirror_HopspaceMovedWhileWaitingForLock(t *testing.T) {
	withGlobalGitConfig(t, map[string]string{"hop.dataLayout": "{host}/{org}/{repo}"})
	root := t.TempDir()
	data := filepath.Join(root, "data")
	withDataHome(t, data)
	fs := afero.NewOsFs()
	wt := filepath.Join(root, "wt")
	writeHook(t, fs, wt, "post-worktree-add", "#!/bin/sh\necho new\n", 0o755)

	oldHopspace := filepath.Join(data, "github.com", "acme", "widgets")
	newHopspace := filepath.Join(data, "acme", "widgets")
	writeFile(t, fs, filepath.Join(oldHopspace, "hop.json"), "{}\n")
	moveBeforeLock(t, fs, oldHopspace, newHopspace)

	res := mirrorMoved(t, fs, wt)

	assertMirroredAfterMove(t, fs, res, filepath.Join(oldHopspace, "hooks"), filepath.Join(newHopspace, "hooks"))
	if ok, _ := afero.Exists(fs, filepath.Join(newHopspace, "hop.json")); !ok {
		t.Error("the moved hopspace must keep its hop.json")
	}
}

// lockCheckingReader answers prompts one read at a time, and at each read
// checks that nobody holds the lock at lockPath.
type lockCheckingReader struct {
	t        *testing.T
	lockPath string
	answers  []string
	reads    int
}

func (r *lockCheckingReader) Read(p []byte) (int, error) {
	r.reads++
	l := filelock.New(r.lockPath)
	ok, err := l.TryAcquire()
	if err != nil {
		r.t.Errorf("try lock: %v", err)
	}
	if !ok {
		r.t.Errorf("the hopspace's hop.json lock is held while a prompt waits (read %d)", r.reads)
	} else {
		_ = l.Release()
	}
	if len(r.answers) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.answers[0])
	r.answers = r.answers[1:]
	return n, nil
}

// Prompts wait on a person; the lock is taken only once every hook is
// decided, and the hooks answered "yes" are then all written.
func TestMirror_PromptsRunOutsideTheLock(t *testing.T) {
	withGlobalGitConfig(t, nil)
	root := t.TempDir()
	data := filepath.Join(root, "data")
	withDataHome(t, data)
	fs := afero.NewOsFs()
	wt := filepath.Join(root, "wt")
	writeHook(t, fs, wt, "post-worktree-add", "#!/bin/sh\n", 0o755)
	writeHook(t, fs, wt, "pre-worktree-add", "#!/bin/sh\n", 0o755)

	stdin := &lockCheckingReader{
		t:        t,
		lockPath: filepath.Join(data, "test-org", "test-repo", hop.HopJSONLockName),
		answers:  []string{"y\n", "y\n"},
	}
	res, err := MirrorCommittedHooks(fs, MirrorOpts{
		WorktreePath: wt, RepoID: testRepoID, Mode: ModePrompt,
		Stdin: stdin, PromptOut: io.Discard,
	})
	if err != nil {
		t.Fatalf("MirrorCommittedHooks: %v", err)
	}
	if stdin.reads < 2 {
		t.Fatalf("prompts read stdin %d times, want 2", stdin.reads)
	}
	if res.Installed != 2 {
		t.Fatalf("Installed = %d, want 2 (%+v)", res.Installed, res.Hooks)
	}
	for _, name := range []string{"post-worktree-add", "pre-worktree-add"} {
		if _, err := os.Lstat(filepath.Join(data, "test-org", "test-repo", "hooks", name)); err != nil {
			t.Errorf("hook %s not mirrored: %v", name, err)
		}
	}
}
