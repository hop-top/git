package hop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
)

// A bare conversion carries the repository's linked worktrees into the
// hub. git keeps each one's state in an admin dir, .git/worktrees/<id>:
// HEAD and its reflog, the index, per-worktree refs (refs/worktree,
// refs/bisect, refs/rewritten), the lock, config.worktree,
// info/sparse-checkout and the git dirs of its submodules. The worktree
// points at that dir through its .git file, and the dir points back
// through its gitdir file.
//
// The conversion copies every admin dir into the hub under the same id,
// before the hub's default worktree is added, so the default is the one
// git renames on a clash. A worktree outside the repository stays where
// it is; one inside the repository's working tree would end up inside
// the default worktree, so it moves to hops/<branch> (hops/<id> when
// detached), as `git hop add` would have put it. See
// conversion_linked_carry.go for the steps.

// linkedAdmin is one entry of .git/worktrees, read as git reads it.
type linkedAdmin struct {
	id, dir string
	// gitdir is the worktree's .git file as the admin dir's gitdir file
	// names it, absolute; empty when that file is missing.
	gitdir  string
	branch  string // "" when HEAD is detached
	locked  bool
	present bool // gitdir exists
}

// path is the worktree's directory.
func (a linkedAdmin) path() string { return filepath.Dir(a.gitdir) }

// prunable is git's own rule (`git worktree list` marks such an entry
// prunable): the gitdir file is missing, or it names a place that is
// gone and the worktree is not locked.
func (a linkedAdmin) prunable() bool {
	return a.gitdir == "" || (!a.present && !a.locked)
}

// readLinkedAdmins lists the admin dirs in repoPath's .git/worktrees, by
// id. A repository with none, or no such directory, has an empty list.
func readLinkedAdmins(fs afero.Fs, repoPath string) ([]linkedAdmin, error) {
	root := filepath.Join(repoPath, ".git", "worktrees")
	entries, err := afero.ReadDir(fs, root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to list %s: %w", root, err)
	}
	var admins []linkedAdmin
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		a := linkedAdmin{id: e.Name(), dir: filepath.Join(root, e.Name())}
		if b, err := afero.ReadFile(fs, filepath.Join(a.dir, "gitdir")); err == nil {
			if g := strings.TrimSpace(string(b)); g != "" {
				if !filepath.IsAbs(g) {
					g = filepath.Join(a.dir, g)
				}
				a.gitdir = filepath.Clean(g)
			}
		}
		if a.gitdir != "" {
			a.present, _ = afero.Exists(fs, a.gitdir)
		}
		a.locked, _ = afero.Exists(fs, filepath.Join(a.dir, "locked"))
		if head, err := afero.ReadFile(fs, filepath.Join(a.dir, "HEAD")); err == nil {
			if ref, ok := strings.CutPrefix(strings.TrimSpace(string(head)), "ref: refs/heads/"); ok {
				a.branch = ref
			}
		}
		admins = append(admins, a)
	}
	return admins, nil
}

// LinkedWorktree is a linked worktree a bare conversion carries.
type LinkedWorktree struct {
	// ID names its admin dir, .git/worktrees/<id> before the conversion
	// and <hub>/worktrees/<id> after it.
	ID string
	// Path is its directory before the conversion.
	Path string
	// Branch is its checked-out branch; empty when HEAD is detached.
	Branch string
	// Locked is git's worktree lock; Present, whether its directory is
	// there. A locked worktree can be absent (an unmounted disk).
	Locked, Present bool
	// Dest is where a worktree inside the repository's working tree
	// moves, relative to the hub; empty for one that stays.
	Dest string

	// realPath is Path with symlinks resolved, taken before anything
	// moves: git computes the relative links of submodules from it.
	realPath string
}

// FinalPath is the worktree's directory once the conversion of the
// repository at hub is done.
func (w LinkedWorktree) FinalPath(hub string) string {
	if w.Dest != "" {
		return filepath.Join(hub, w.Dest)
	}
	return w.Path
}

// LinkedCarryPlan is what a bare conversion does with the repository's
// linked worktrees.
type LinkedCarryPlan struct {
	Worktrees []LinkedWorktree
	// Prunable are the directories of entries git would prune: left
	// behind.
	Prunable []string

	// realRepo is the repository path with symlinks resolved.
	realRepo string
}

// StatusExcludes are pathspecs, relative to the repository, that keep
// the worktrees about to move out of its working tree out of `git
// status`: git lists a nested worktree as an untracked directory, but
// the conversion carries it rather than the main worktree's content.
func (p *LinkedCarryPlan) StatusExcludes(repoPath string) []string {
	var specs []string
	for _, w := range p.Worktrees {
		if w.Dest == "" {
			continue
		}
		if rel, err := filepath.Rel(repoPath, w.Path); err == nil {
			specs = append(specs, ":(exclude)"+filepath.ToSlash(rel))
		}
	}
	return specs
}

// ErrLinkedWorktrees is the refusal of a bare conversion of a repository
// with a linked worktree it cannot carry; the reasons are in a
// *LinkedWorktreesError.
var ErrLinkedWorktrees = errors.New("linked worktrees cannot be carried into the hub")

// LinkedWorktreesError lists why a bare conversion cannot carry the
// repository's linked worktrees, one reason per worktree problem.
type LinkedWorktreesError struct {
	Problems []string
}

func (e *LinkedWorktreesError) Error() string { return ErrLinkedWorktrees.Error() }

func (e *LinkedWorktreesError) Unwrap() error { return ErrLinkedWorktrees }

// PlanLinkedCarry reads repoPath's linked worktrees and decides what a
// bare conversion does with each, before anything moves. It returns a
// *LinkedWorktreesError when one cannot be carried: its admin dir is
// busy, it sits where the conversion works, it shares a branch with
// another worktree, or it is inside another linked worktree. A paused
// operation is refused separately (CheckNoOperationInProgress).
func PlanLinkedCarry(fs afero.Fs, g git.GitInterface, repoPath string) (*LinkedCarryPlan, error) {
	admins, err := readLinkedAdmins(fs, repoPath)
	if err != nil {
		return nil, err
	}
	plan := &LinkedCarryPlan{realRepo: realPath(repoPath)}
	if len(admins) == 0 {
		return plan, nil
	}

	var problems []string
	// Each branch and each hops/ destination has one owner.
	branchAt := map[string]string{}
	var dests []string
	if def, err := CurrentBranchForConversion(g, repoPath); err == nil && def != "" {
		branchAt[def] = repoPath
		dests = append(dests, config.MakeWorktreePath(def))
	}
	gitDir := filepath.Join(repoPath, ".git")
	scratch := []string{repoPath + ".new", repoPath + ".old"}

	for _, a := range admins {
		if a.prunable() {
			if a.gitdir != "" {
				plan.Prunable = append(plan.Prunable, a.path())
			} else {
				plan.Prunable = append(plan.Prunable, a.dir)
			}
			continue
		}
		w := LinkedWorktree{ID: a.id, Path: a.path(), Branch: a.branch, Locked: a.locked, Present: a.present}
		w.realPath = realPath(w.Path)

		if pathWithin(w.Path, gitDir) {
			problems = append(problems, fmt.Sprintf("%s: inside %s, which the conversion replaces", w.Path, gitDir))
		}
		for _, s := range scratch {
			if pathWithin(w.Path, s) || pathWithin(s, w.Path) {
				problems = append(problems, fmt.Sprintf("%s: in the way of %s, where the conversion builds the hub", w.Path, s))
			}
		}
		if ok, _ := afero.Exists(fs, filepath.Join(a.dir, "index.lock")); ok {
			problems = append(problems, fmt.Sprintf("%s: %s exists; another git process seems to be running there. "+
				"If none is, remove the file", w.Path, filepath.Join(a.dir, "index.lock")))
		}
		if w.Branch != "" {
			if other, taken := branchAt[w.Branch]; taken {
				problems = append(problems, fmt.Sprintf("%s: branch %s is also checked out at %s; a hub has one worktree per branch",
					w.Path, w.Branch, other))
			} else {
				branchAt[w.Branch] = w.Path
			}
		}
		if w.Present && pathWithin(w.Path, repoPath) && !pathWithin(w.Path, gitDir) {
			name := w.Branch
			if name == "" {
				name = w.ID
			}
			w.Dest = config.MakeWorktreePath(name)
			for _, d := range dests {
				if lexicallyWithin(w.Dest, d) || lexicallyWithin(d, w.Dest) {
					problems = append(problems, fmt.Sprintf("%s: would move to %s, which clashes with %s", w.Path, w.Dest, d))
				}
			}
			dests = append(dests, w.Dest)
		}
		plan.Worktrees = append(plan.Worktrees, w)
	}

	for _, inner := range plan.Worktrees {
		for _, outer := range plan.Worktrees {
			if inner.ID != outer.ID && pathWithin(inner.Path, outer.Path) {
				problems = append(problems, fmt.Sprintf("%s: inside the linked worktree %s", inner.Path, outer.Path))
			}
		}
	}

	if len(problems) > 0 {
		return plan, &LinkedWorktreesError{Problems: problems}
	}
	return plan, nil
}

// liveLinkedAdmins are the admin dirs of the linked worktrees a bare
// conversion carries: every entry but the prunable ones.
func liveLinkedAdmins(fs afero.Fs, repoPath string) []linkedAdmin {
	admins, _ := readLinkedAdmins(fs, repoPath)
	var live []linkedAdmin
	for _, a := range admins {
		if !a.prunable() {
			live = append(live, a)
		}
	}
	return live
}

// lexicallyWithin reports whether the relative path p is dir or under it,
// comparing names only.
func lexicallyWithin(p, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(p))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// StatusPorcelainArgs is `git status --porcelain` for the clean check of
// a conversion that follows plan (nil for none): the linked worktrees
// about to move out of the working tree are excluded.
func StatusPorcelainArgs(plan *LinkedCarryPlan, repoPath string) []string {
	args := []string{"status", "--porcelain"}
	if plan == nil {
		return args
	}
	if ex := plan.StatusExcludes(repoPath); len(ex) > 0 {
		args = append(append(args, "--"), ex...)
	}
	return args
}
