package hop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/afero"
)

// A bare conversion clones the repository into a new hub, then swaps the
// old folder, .git directory included, out. `git clone --bare` brings
// the objects (alternates and a shallow boundary included), the branches,
// the tags and HEAD; config, remotes and the working tree have their own
// steps. What else lives in the old .git directory is decided here, entry
// by entry, for every top-level name git writes there.
//
// Carried into the hub, moved rather than copied since the old .git is
// deleted next (the conversion backup keeps its own copy):
var gitDirCarried = map[string]string{
	"info": "exclude, attributes and the other files under info/ apply to " +
		"every worktree of a bare hub, as they applied to the only worktree " +
		"before; sparse-checkout is per-worktree and goes to the default " +
		"worktree's own git dir; info/refs is left out, update-server-info " +
		"regenerates it",
	"hooks": "the user's git hooks; the clone's *.sample files stay as " +
		"they are and the old ones are not copied. git-hop's own hooks " +
		"never live here",
	"description": "read by gitweb and mail hooks",
	"rr-cache":    "rerere's recorded conflict resolutions",
	"lfs":         "the Git LFS object store, which can hold content not pushed yet",
	"branches":    "legacy remote definitions git still reads",
	"remotes":     "legacy remote definitions git still reads",
	"refs": "refs the clone does not copy: notes, replace refs, the stash " +
		"and any other namespace; per-worktree and derived ones excepted " +
		"(see carryOverExtraRefs)",
	"packed-refs": "same as refs",
	"config.worktree": "per-worktree config: with extensions.worktreeConfig " +
		"on, its keys go to the default worktree's own config.worktree and " +
		"the hub's core.bare moves to the hub's (see carryOverWorktreeConfig); " +
		"with it off git did not read the file, and it is left behind with a warning",
	"modules": "the submodules' git dirs, moved to the default worktree's " +
		"own git dir and reconnected (see carryOverSubmodules)",
	"logs": "reflogs: every stash entry but the latest lives only in the " +
		"stash reflog, and the others are the user's undo history. " +
		"logs/HEAD belongs to the default worktree",
}

// Left behind without a word: another step owns them, or they are
// derived data or the state of an operation that should not span a
// conversion.
var gitDirLeftBehind = map[string]string{
	"objects":               "the clone copies every object, alternates included",
	"HEAD":                  "the clone's HEAD names the same branch",
	"config":                "carried key by key (conversion_config.go, conversion_remotes.go)",
	"shallow":               "the clone of a shallow repository writes its own",
	"index":                 "per-worktree; rebuilt in the default worktree from the staged diff (conversion_index.go)",
	"FETCH_HEAD":            "rewritten by the next fetch",
	"ORIG_HEAD":             "left by the last reset, merge or rebase",
	"AUTO_MERGE":            "operation state",
	"MERGE_MSG":             "operation state",
	"MERGE_MODE":            "operation state",
	"MERGE_RR":              "rerere state of a merge in progress",
	"SQUASH_MSG":            "operation state",
	"COMMIT_EDITMSG":        "editor scratch file",
	"TAG_EDITMSG":           "editor scratch file",
	"NOTES_EDITMSG":         "editor scratch file",
	"EDIT_DESCRIPTION":      "editor scratch file",
	"gc.pid":                "lock of a running gc",
	"gc.log":                "log of the last auto gc",
	"gitk.cache":            "cache",
	"fsmonitor--daemon":     "runtime state of the fsmonitor daemon",
	"fsmonitor--daemon.ipc": "runtime state of the fsmonitor daemon",
	"worktrees": "admin dirs of linked worktrees: a bare conversion refuses " +
		"a repository with live ones (refuseLinkedWorktrees), so only " +
		"prunable entries are left",
}

// Left-behind name prefixes, for entries whose names carry a suffix.
// Checked after gitDirWarned.
var gitDirLeftBehindPrefixes = map[string]string{
	"BISECT_":      "bisect state; BISECT_START and BISECT_LOG mark a bisect in progress",
	"sharedindex.": "split index of the old index",
}

// Left behind with a warning: user state this conversion does not carry.
var gitDirWarned = map[string]string{}

// Refused up front: the markers of an operation in progress
// (gitDirInProgress: MERGE_HEAD, rebase-merge, sequencer, BISECT_START,
// ...). Their state is per-worktree and names the old layout, so a bare
// conversion refuses to start while one is present (see
// CheckNoOperationInProgress). Should one appear mid-conversion anyway,
// it is warned about, never dropped in silence.

// gitDirCarry names the three places a carry moves things between.
type gitDirCarry struct {
	src         string // the old .git directory
	hub         string // the new bare hub
	worktreeGit string // the default worktree's own git dir in the hub
}

// carryOverGitDir moves the user state of repoPath's .git directory into
// the hub at bareRepo, and returns a warning for each entry it leaves
// behind that the user may miss. worktreePath is the default worktree's
// checkout, worktreeGitDir its git dir (<hub>/worktrees/<name>).
func (c *Converter) carryOverGitDir(repoPath, bareRepo, worktreePath, worktreeGitDir string) ([]string, error) {
	gc := gitDirCarry{src: filepath.Join(repoPath, ".git"), hub: bareRepo, worktreeGit: worktreeGitDir}

	entries, err := afero.ReadDir(c.fs, gc.src)
	if err != nil {
		return nil, fmt.Errorf("failed to list %s: %w", gc.src, err)
	}
	var warnings []string
	for _, e := range entries {
		if w := gitDirWarning(e.Name()); w != "" {
			warnings = append(warnings, w)
		}
	}

	for _, name := range []string{"description", "rr-cache", "lfs", "branches", "remotes"} {
		if err := c.moveIfExists(filepath.Join(gc.src, name), filepath.Join(gc.hub, name)); err != nil {
			return nil, fmt.Errorf("failed to carry over .git/%s: %w", name, err)
		}
	}
	if err := c.carryOverInfo(gc); err != nil {
		return nil, err
	}
	if err := c.carryOverHooks(gc); err != nil {
		return nil, err
	}
	if err := c.carryOverSubmodules(repoPath, worktreePath, worktreeGitDir); err != nil {
		return nil, err
	}
	if err := c.carryOverExtraRefs(repoPath, bareRepo); err != nil {
		return nil, err
	}
	// After the refs: a reflog is only meaningful next to its ref, and the
	// fetch above would otherwise log itself over the carried history.
	w, err := c.carryOverReflogs(repoPath, gc)
	if err != nil {
		return nil, err
	}
	return append(warnings, w...), nil
}

// gitDirWarning returns the warning for a .git entry left behind, or ""
// when the entry is carried or needs no carrying.
func gitDirWarning(name string) string {
	if _, ok := gitDirCarried[name]; ok {
		return ""
	}
	if w, ok := gitDirWarned[name]; ok {
		return fmt.Sprintf(".git/%s: %s", name, w)
	}
	if isInProgressMarker(name) {
		return fmt.Sprintf(".git/%s: an operation was in progress and is abandoned", name)
	}
	if _, ok := gitDirLeftBehind[name]; ok {
		return ""
	}
	for prefix := range gitDirLeftBehindPrefixes {
		if strings.HasPrefix(name, prefix) {
			return ""
		}
	}
	return fmt.Sprintf(".git/%s: not carried over; git-hop does not know what it holds", name)
}

func (c *Converter) moveIfExists(src, dst string) error {
	if _, err := c.fs.Stat(src); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := c.fs.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return c.mergeRename(src, dst)
}

// carryOverInfo moves .git/info/* into the hub's info/, where it applies
// to every linked worktree, except the per-worktree sparse-checkout.
func (c *Converter) carryOverInfo(gc gitDirCarry) error {
	infoDir := filepath.Join(gc.src, "info")
	entries, err := afero.ReadDir(c.fs, infoDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to list %s: %w", infoDir, err)
	}
	for _, e := range entries {
		dst := filepath.Join(gc.hub, "info", e.Name())
		switch e.Name() {
		case "refs":
			continue
		case "sparse-checkout":
			dst = filepath.Join(gc.worktreeGit, "info", e.Name())
		}
		if err := c.moveIfExists(filepath.Join(infoDir, e.Name()), dst); err != nil {
			return fmt.Errorf("failed to carry over .git/info/%s: %w", e.Name(), err)
		}
	}
	return nil
}

// carryOverHooks moves the user's hooks into the hub's hooks/, which
// every linked worktree runs. Sample hooks are the clone's own.
func (c *Converter) carryOverHooks(gc gitDirCarry) error {
	hooksDir := filepath.Join(gc.src, "hooks")
	entries, err := afero.ReadDir(c.fs, hooksDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to list %s: %w", hooksDir, err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sample") {
			continue
		}
		if err := c.moveIfExists(filepath.Join(hooksDir, e.Name()), filepath.Join(gc.hub, "hooks", e.Name())); err != nil {
			return fmt.Errorf("failed to carry over .git/hooks/%s: %w", e.Name(), err)
		}
	}
	return nil
}

// Ref namespaces carryOverExtraRefs leaves to others. The clone copies
// heads and tags, and carryOverRemotes the remote-tracking refs; their
// reflogs are still carried.
var refNamespacesOwnedElsewhere = []string{"refs/heads/", "refs/tags/", "refs/remotes/"}

// Ref namespaces left behind with their reflogs: bisect, worktree and
// rewritten are per-worktree operation state, and prefetch is git
// maintenance's rebuildable copy of the remotes.
var refNamespacesLeftBehind = []string{"refs/bisect/", "refs/worktree/", "refs/rewritten/", "refs/prefetch/"}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// carryOverExtraRefs fetches the refs a bare clone does not copy (notes,
// replace refs, refs/stash, ...) from srcRepo into bareRepo, one refspec
// per namespace.
func (c *Converter) carryOverExtraRefs(srcRepo, bareRepo string) error {
	out, err := c.git.Run("git", "-C", srcRepo, "for-each-ref", "--format=%(refname)")
	if err != nil {
		return fmt.Errorf("failed to list refs of %s: %w", srcRepo, err)
	}
	seen := map[string]bool{}
	var refspecs []string
	for _, ref := range strings.Split(out, "\n") {
		if !strings.HasPrefix(ref, "refs/") || hasAnyPrefix(ref, refNamespacesOwnedElsewhere) || hasAnyPrefix(ref, refNamespacesLeftBehind) {
			continue
		}
		spec := "+" + ref + ":" + ref
		if parts := strings.SplitN(ref, "/", 3); len(parts) == 3 {
			ns := parts[0] + "/" + parts[1] + "/*"
			spec = "+" + ns + ":" + ns
		}
		if !seen[spec] {
			seen[spec] = true
			refspecs = append(refspecs, spec)
		}
	}
	if len(refspecs) == 0 {
		return nil
	}
	sort.Strings(refspecs)
	args := append([]string{"-C", bareRepo, "fetch", "--no-tags", "--quiet", srcRepo}, refspecs...)
	if _, err := c.git.Run("git", args...); err != nil {
		return fmt.Errorf("failed to carry over refs %v: %w", refspecs, err)
	}
	return nil
}

// carryOverReflogs moves the old reflogs into the hub: logs/refs/* to the
// hub's own, logs/HEAD ahead of the default worktree's HEAD reflog, which
// so far only records its checkout. A local clone copies every object,
// so every entry still resolves; a shallow repository is cloned through
// the transport instead, which leaves out what no ref reaches, so its
// reflogs stay behind.
func (c *Converter) carryOverReflogs(srcRepo string, gc gitDirCarry) ([]string, error) {
	logsDir := filepath.Join(gc.src, "logs")
	if _, err := c.fs.Stat(logsDir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if shallow, _ := c.git.Run("git", "-C", srcRepo, "rev-parse", "--is-shallow-repository"); shallow == "true" {
		return []string{".git/logs: reflogs not carried over: the repository is shallow, " +
			"so the hub may lack the commits they name; only the latest stash is kept"}, nil
	}

	refLogs := filepath.Join(logsDir, "refs")
	entries, err := afero.ReadDir(c.fs, refLogs)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("failed to list %s: %w", refLogs, err)
	}
	for _, e := range entries {
		ref := "refs/" + e.Name()
		if e.IsDir() {
			ref += "/"
		}
		if hasAnyPrefix(ref, refNamespacesLeftBehind) {
			continue
		}
		if err := c.moveIfExists(filepath.Join(refLogs, e.Name()), filepath.Join(gc.hub, "logs", "refs", e.Name())); err != nil {
			return nil, fmt.Errorf("failed to carry over reflogs of %s: %w", ref, err)
		}
	}

	return nil, c.prependFile(filepath.Join(logsDir, "HEAD"), filepath.Join(gc.worktreeGit, "logs", "HEAD"))
}

// prependFile writes src's content ahead of dst's, creating dst if needed.
func (c *Converter) prependFile(src, dst string) error {
	old, err := afero.ReadFile(c.fs, src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	cur, err := afero.ReadFile(c.fs, dst)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := c.fs.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return afero.WriteFile(c.fs, dst, append(old, cur...), 0o644)
}
