package hooks

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// gitHub creates a bare repository standing in for a hub, whose local
// config holds kv: hop.dataLayout is read from it with git.
func gitHub(t *testing.T, kv map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "--bare", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for k, v := range kv {
		if out, err := exec.Command("git", "-C", dir, "config", k, v).CombinedOutput(); err != nil {
			t.Fatalf("git config %s: %v\n%s", k, err, out)
		}
	}
	return dir
}

// recordHubs records hubs as the hubs of movedRepoID in state.
func recordHubs(t *testing.T, fs afero.Fs, hubs ...string) {
	t.Helper()
	st := state.NewState()
	rs := &state.RepositoryState{URI: movedURI, Org: "acme", Repo: "widgets"}
	for _, h := range hubs {
		rs.Hubs = append(rs.Hubs, &state.HubState{Path: h, Mode: state.HubModeLocal})
	}
	st.AddRepository(movedRepoID, rs)
	if err := state.SaveState(fs, st); err != nil {
		t.Fatalf("save state: %v", err)
	}
}

// mirrorFrom mirrors the committed hook of the worktree wt with stderr
// captured.
func mirrorFrom(t *testing.T, fs afero.Fs, wt string) (Result, string) {
	t.Helper()
	var res Result
	var err error
	stderr := captureOSStderr(t, func() {
		output.SetupLogger(output.ModeHuman, false)
		res, err = MirrorCommittedHooks(fs, MirrorOpts{
			WorktreePath: wt, RepoID: movedRepoID, RepoURI: movedURI, Mode: ModeCopy,
		})
	})
	output.SetupLogger(output.ModeHuman, false)
	if err != nil {
		t.Fatalf("MirrorCommittedHooks: %v", err)
	}
	return res, stderr
}

// A hub with its own hop.dataLayout mirrors where that puts the hooks,
// and warns that another hub of the repository puts them elsewhere,
// naming each hub's value and how to align them.
func TestMirror_WarnsWhenHubsResolveLayoutDifferently(t *testing.T) {
	withGlobalGitConfig(t, nil)
	data := filepath.Join(t.TempDir(), "data")
	withDataHome(t, data)
	fs := afero.NewOsFs()
	hubA := gitHub(t, map[string]string{"hop.dataLayout": "{host}/{org}/{repo}"})
	hubB := gitHub(t, nil)
	recordHubs(t, fs, hubB, hubA)
	writeHook(t, fs, hubA, "post-worktree-add", "#!/bin/sh\necho new\n", 0o755)

	res, stderr := mirrorFrom(t, fs, hubA)

	if res.Installed != 1 {
		t.Fatalf("Installed = %d, want 1 (%+v)", res.Installed, res.Hooks)
	}
	own := filepath.Join(data, "github.com", "acme", "widgets", "hooks", "post-worktree-add")
	if ok, _ := afero.Exists(fs, own); !ok {
		t.Errorf("hook not mirrored where this hub's hop.dataLayout puts it (%s)", own)
	}
	if ok, _ := afero.Exists(fs, filepath.Join(data, "acme", "widgets", "hooks")); ok {
		t.Error("hook mirrored where another hub's hop.dataLayout puts it: a guess")
	}
	for _, want := range []string{
		"warning: hubs of acme/widgets resolve hop.dataLayout to different hopspaces",
		"hint:   " + hubA + ": {host}/{org}/{repo} (local config)",
		"hint:   " + hubB + ": {org}/{repo} (default)",
		"hint:   git config --global hop.dataLayout <layout>",
		"hint:   git -C " + hubA + " config --unset hop.dataLayout",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, stderr)
		}
	}
}

// Hubs that agree get no warning.
func TestMirror_NoLayoutWarningWhenHubsAgree(t *testing.T) {
	withGlobalGitConfig(t, nil)
	withDataHome(t, filepath.Join(t.TempDir(), "data"))
	fs := afero.NewOsFs()
	hubA := gitHub(t, nil)
	hubB := gitHub(t, nil)
	recordHubs(t, fs, hubB, hubA)
	writeHook(t, fs, hubA, "post-worktree-add", "#!/bin/sh\necho new\n", 0o755)

	res, stderr := mirrorFrom(t, fs, hubA)

	if res.Installed != 1 {
		t.Fatalf("Installed = %d, want 1 (%+v)", res.Installed, res.Hooks)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want none", stderr)
	}
}

// moveOnlyBeforeLock makes the next mirror's first lock wait meet a move
// of from to to under the lock of the hopspace the mirror resolved,
// hop.dataLayout left as it is: the move followed another hub's.
func moveOnlyBeforeLock(t *testing.T, fs afero.Fs, from, to string) {
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

// A move of the hooks dir while the mirror waited, to where another hub
// resolves it, leaves this worktree resolving the old path still. The
// mirror writes nothing there: hooks never land where a move just took
// them from.
func TestMirror_RefusesDirMovedAwayItStillResolves(t *testing.T) {
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
	moveOnlyBeforeLock(t, fs, oldHooks, newHooks)

	res, stderr := mirrorFrom(t, fs, wt)

	if res.Installed != 0 || res.Warned != 1 {
		t.Errorf("Installed = %d, Warned = %d, want 0 and 1 (%+v)", res.Installed, res.Warned, res.Hooks)
	}
	if ok, _ := afero.Exists(fs, oldHooks); ok {
		t.Errorf("hooks landed at %s, where the move took them from", oldHooks)
	}
	if ok, _ := afero.Exists(fs, filepath.Join(newHooks, "post-worktree-add")); ok {
		t.Error("hook mirrored where this worktree's hop.dataLayout does not put it: a guess")
	}
	if !strings.Contains(stderr, "warning: committed hooks were not mirrored: "+oldHooks+" was moved away") {
		t.Errorf("stderr lacks the refusal:\n%s", stderr)
	}
}

// Taking the lock of a hopspace a move took away recreates its directory
// empty. The mirror writes where the hopspace went and removes the
// directory it recreated.
func TestMirror_HopspaceMovedLeavesNoEmptyDir(t *testing.T) {
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
	if ok, _ := afero.Exists(fs, oldHopspace); ok {
		t.Errorf("taking the lock left an empty %s behind", oldHopspace)
	}
	if ok, _ := afero.DirExists(fs, filepath.Dir(oldHopspace)); !ok {
		t.Error("the parent the move left must stay")
	}
}

// When the hooks dir alone moved, its hopspace dir is the one the mirror
// saw, not one it made: it stays, even emptied.
func TestMirror_HooksDirMovedKeepsEmptiedHopspace(t *testing.T) {
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
	moveBeforeLock(t, fs, oldHooks, newHooks)

	res := mirrorMoved(t, fs, wt)

	assertMirroredAfterMove(t, fs, res, oldHooks, newHooks)
	if ok, _ := afero.DirExists(fs, filepath.Dir(oldHooks)); !ok {
		t.Errorf("%s was removed; the mirror did not create it", filepath.Dir(oldHooks))
	}
}
