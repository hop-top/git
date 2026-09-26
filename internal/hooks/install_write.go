package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
)

// The hopspace hooks dir is hopspace data like hop.json, and doctor --fix
// moves it (with the whole hopspace, or on its own from where releases
// before hop.dataLayout mirrored it) under that hopspace's hop.json lock.
// So a mirror writes its hooks under the same lock: a move waits for the
// hooks being written and carries them, and a mirror that starts during
// a move waits for it.
//
// The lock is held only around the writes. Every hook is decided first,
// prompts included, and the hooks dir is resolved (hop.dataLayout, which
// runs git config) before the lock is taken. Nothing under it runs git,
// a hook or git-hop.

// plannedHook is a hook MirrorCommittedHooks decided to install.
type plannedHook struct {
	name string
	src  string
	info os.FileInfo
	mode string // ModeSymlink or ModeCopy
	// replace allows replacing a hook with different content found at the
	// destination: --hooks-overwrite, or a prompt answered for a hook
	// that already differed. Otherwise such a hook is kept, with a
	// warning.
	replace bool
}

// mirrorAttempts bounds how often installPlanned resolves the hooks dir
// again after finding, once it held the lock, that the dir had been moved
// away while it waited.
const mirrorAttempts = 3

// beforeMirrorLock runs after installPlanned has resolved the hooks dir
// and looked at it, just before it takes the lock. Tests replace it to
// move the dir meanwhile.
var beforeMirrorLock = func(hooksDir string) {}

// installPlanned creates the hooks dir hop.dataLayout resolves for the
// worktree and writes the planned hooks into it, holding the hop.json lock
// of the hopspace that dir belongs to. A dir that held data before the
// wait and none once the lock is held was moved away meanwhile (doctor
// --fix): nothing is written there, and the dir is resolved again. When
// it resolves to the same dir, this worktree's hop.dataLayout disagrees
// with the one the move followed, and nothing is written at all: the
// hooks would land right where the move took them from.
func installPlanned(fs afero.Fs, worktreePath string, ref hop.RepoRef, plan []plannedHook, res *Result) error {
	var movedFrom string
	for attempt := 1; ; attempt++ {
		hooksDir := hop.HopspaceHooksDir(ref.In(worktreePath))
		if movedFrom != "" && filepath.Clean(hooksDir) == filepath.Clean(movedFrom) {
			notMirrored(plan, res, "hooks dir moved away; hop.dataLayout still puts it here")
			output.Warn("committed hooks were not mirrored: %s was moved away while they were written, "+
				"but this worktree's hop.dataLayout still puts them there", hooksDir)
			output.Hint("Another hub of this repository resolves hop.dataLayout differently.\n"+
				"Give every hub the same value ('git config --global %s <layout>', and\n"+
				"'git config --unset %s' in a hub that overrides it), then run %s.",
				config.KeyDataLayout, config.KeyDataLayout, remirrorCommand(worktreePath, ModeSymlink))
			return nil
		}
		hopspace := filepath.Dir(hooksDir)
		seen := seeHooksDir(fs, hooksDir)
		beforeMirrorLock(hooksDir)

		moved := false
		err := hop.WithHopJSONLock(fs, hopspace, func() error {
			if seen.movedAway(fs, hooksDir) {
				moved = true
				dropRecreatedHopspace(fs, hopspace, seen)
				return nil
			}
			if err := fs.MkdirAll(hooksDir, 0755); err != nil {
				return fmt.Errorf("create hopspace hooks dir: %w", err)
			}
			for _, h := range plan {
				writePlanned(fs, h, hooksDir, res)
			}
			return nil
		})
		if err != nil || !moved {
			return err
		}
		movedFrom = hooksDir
		if attempt == mirrorAttempts {
			notMirrored(plan, res, "hopspace moved while mirroring")
			output.Warn("committed hooks were not mirrored: %s kept moving while they were written", hooksDir)
			output.Hint("Run %s to mirror them.", remirrorCommand(worktreePath, ModeSymlink))
			return nil
		}
	}
}

// notMirrored records every planned hook as not written, for reason.
func notMirrored(plan []plannedHook, res *Result, reason string) {
	for _, h := range plan {
		res.Warned++
		res.Hooks = append(res.Hooks, HookOutcome{Name: h.name, Status: "warned", Reason: reason})
	}
}

// writePlanned installs h in hooksDir, checking the destination again: it
// may have changed since h was decided.
func writePlanned(fs afero.Fs, h plannedHook, hooksDir string, res *Result) {
	dst := filepath.Join(hooksDir, h.name)
	if exists, _ := afero.Exists(fs, dst); exists {
		if same, err := filesIdentical(fs, h.src, dst); err == nil && same {
			res.AlreadyPresent++
			res.Hooks = append(res.Hooks, HookOutcome{Name: h.name, Status: "already-present"})
			return
		}
		if !h.replace {
			res.Warned++
			res.Hooks = append(res.Hooks, HookOutcome{Name: h.name, Status: "warned", Reason: "exists, no overwrite"})
			output.Warn("hopspace hook %s already exists with different content; pass --hooks-overwrite to replace", h.name)
			return
		}
	}
	if err := installHook(fs, h.mode, h.src, dst, h.info); err != nil {
		res.Warned++
		res.Hooks = append(res.Hooks, HookOutcome{Name: h.name, Status: "warned", Reason: err.Error()})
		output.Warn("failed to install hook %s: %v", h.name, err)
		return
	}
	res.Installed++
	res.Hooks = append(res.Hooks, HookOutcome{Name: h.name, Status: "installed", Target: dst})
}

// hooksDirSeen is what installPlanned saw of a hooks dir and its hopspace
// before waiting for the lock.
type hooksDirSeen struct {
	hooks bool        // the hooks dir existed
	data  bool        // the hopspace held something besides lock files
	dir   os.FileInfo // the hopspace dir; nil when it did not exist
}

func seeHooksDir(fs afero.Fs, hooksDir string) hooksDirSeen {
	hooks, _ := afero.DirExists(fs, hooksDir)
	dir, _ := fs.Stat(filepath.Dir(hooksDir))
	return hooksDirSeen{hooks: hooks, data: holdsData(fs, filepath.Dir(hooksDir)), dir: dir}
}

// movedAway reports whether what was seen at hooksDir is gone: the hooks
// dir, or everything in its hopspace. Taking the lock recreates an empty
// hopspace dir, holding only the lock file, where a move took it away.
func (s hooksDirSeen) movedAway(fs afero.Fs, hooksDir string) bool {
	if s.hooks {
		if ok, _ := afero.DirExists(fs, hooksDir); !ok {
			return true
		}
	}
	return s.data && !holdsData(fs, filepath.Dir(hooksDir))
}

// dropRecreatedHopspace removes the directory at hopspace that taking its
// lock made anew, after a move took away the one seen there: it is not
// that directory, and it holds the hop.json lock file this run holds and
// nothing else. installPlanned calls it under that lock, having found the
// hopspace moved away.
//
// Nothing but that lock file is ever removed, and the directory only by
// rmdir, which refuses one anything was put in. Another run taking the
// lock meanwhile either opened the lock file first and waits on it (this
// run holds it; its retry then finds the path gone, and makes the
// directory and a lock file afresh), or creates its own lock file once
// this one is unlinked, which makes the rmdir fail and the directory
// stay. A directory that is the one seen (the hooks dir alone moved out
// of it), or that cannot be told apart from it, stays too: this run did
// not make it.
func dropRecreatedHopspace(fs afero.Fs, hopspace string, seen hooksDirSeen) {
	now, err := fs.Stat(hopspace)
	if err != nil || !recreatedDir(seen.dir, now) {
		return
	}
	entries, err := afero.ReadDir(fs, hopspace)
	if err != nil || len(entries) != 1 || entries[0].Name() != hop.HopJSONLockName {
		return
	}
	if err := fs.Remove(filepath.Join(hopspace, hop.HopJSONLockName)); err != nil {
		return
	}
	_ = fs.Remove(hopspace)
}

// recreatedDir reports whether now is another directory than seen, the
// one at the same path before: seen is nil when there was none. A
// filesystem that cannot tell (no file identity, as in memory) counts
// as the same directory.
func recreatedDir(seen, now os.FileInfo) bool {
	if seen == nil {
		return true
	}
	if seen.Sys() == nil || now.Sys() == nil {
		return false
	}
	return !os.SameFile(seen, now)
}

// holdsData reports whether dir holds an entry other than a lock file.
func holdsData(fs afero.Fs, dir string) bool {
	entries, err := afero.ReadDir(fs, dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".lock") {
			return true
		}
	}
	return false
}
