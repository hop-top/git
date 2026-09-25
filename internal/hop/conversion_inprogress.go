package hop

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

// GitOperation is a git command stopped midway: a merge waiting for its
// commit, a rebase at an edit stop, a bisect, and so on. Its state lives
// in the git dir of the one worktree it runs in, and names that
// worktree's layout, so a bare conversion cannot carry it.
type GitOperation struct {
	Name     string // as git words it: "merge", "rebase", "cherry-pick", ...
	Continue string // the command that finishes it; "" when it only ends
	Abort    string // the command that abandons it and restores the start
}

// Hint is git-style advice to conclude the operation before retrying.
func (op GitOperation) Hint() string {
	if op.Continue == "" {
		return fmt.Sprintf("end the %s with '%s'", op.Name, op.Abort)
	}
	return fmt.Sprintf("finish the %s with '%s' or abort it with '%s'", op.Name, op.Continue, op.Abort)
}

// hintIn is Hint for an operation paused in the worktree at dir, its
// commands run there with `git -C <dir>`.
func (op GitOperation) hintIn(dir string) string {
	in := func(cmd string) string {
		if rest, ok := strings.CutPrefix(cmd, "git "); ok {
			return "git -C " + shellQuote(dir) + " " + rest
		}
		return cmd
	}
	return GitOperation{Name: op.Name, Continue: in(op.Continue), Abort: in(op.Abort)}.Hint()
}

// shellQuote quotes s for a POSIX shell when it needs it.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`!*?[]{}()<>|&;#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

var (
	opMerge      = GitOperation{"merge", "git merge --continue", "git merge --abort"}
	opRebase     = GitOperation{"rebase", "git rebase --continue", "git rebase --abort"}
	opAm         = GitOperation{"am", "git am --continue", "git am --abort"}
	opCherryPick = GitOperation{"cherry-pick", "git cherry-pick --continue", "git cherry-pick --abort"}
	opRevert     = GitOperation{"revert", "git revert --continue", "git revert --abort"}
	// A sequence whose todo list does not say which; the sequencer is
	// shared, so cherry-pick's commands conclude a revert sequence too.
	opSequence = GitOperation{"cherry-pick or revert", "git cherry-pick --continue", "git cherry-pick --abort"}
	opBisect   = GitOperation{"bisect", "", "git bisect reset"}
)

// gitDirInProgress maps each git-dir entry whose presence means an
// operation is in progress to that operation, the same markers git's
// own status reads. A bare conversion refuses while any is present (see
// InProgressOperations); the .git decision table consults this map, so
// the refusal and the table cannot disagree on what a marker is.
// Checked in this order; the first marker of an operation names it.
var gitDirInProgress = []struct {
	entry string
	op    func(gitDir string, fs afero.Fs) GitOperation
}{
	{"rebase-merge", fixedOp(opRebase)},
	{"rebase-apply", rebaseApplyOp},
	{"MERGE_HEAD", fixedOp(opMerge)},
	{"CHERRY_PICK_HEAD", fixedOp(opCherryPick)},
	{"REVERT_HEAD", fixedOp(opRevert)},
	{"sequencer", sequencerOp},
	{"BISECT_START", fixedOp(opBisect)},
	{"BISECT_LOG", fixedOp(opBisect)},
}

func fixedOp(op GitOperation) func(string, afero.Fs) GitOperation {
	return func(string, afero.Fs) GitOperation { return op }
}

// rebaseApplyOp tells git am from a rebase on the apply backend: both
// keep their state in rebase-apply/, and am marks it with "applying".
func rebaseApplyOp(gitDir string, fs afero.Fs) GitOperation {
	if ok, _ := afero.Exists(fs, filepath.Join(gitDir, "rebase-apply", "applying")); ok {
		return opAm
	}
	return opRebase
}

// sequencerOp names a multi-commit cherry-pick or revert that has no
// CHERRY_PICK_HEAD or REVERT_HEAD, e.g. after a conflict was committed
// but before --continue, from the first command of its todo list.
func sequencerOp(gitDir string, fs afero.Fs) GitOperation {
	todo, err := afero.ReadFile(fs, filepath.Join(gitDir, "sequencer", "todo"))
	if err != nil {
		return opSequence
	}
	first := strings.Fields(string(todo))
	switch {
	case len(first) == 0:
		return opSequence
	case first[0] == "pick" || first[0] == "p":
		return opCherryPick
	case first[0] == "revert":
		return opRevert
	}
	return opSequence
}

// isInProgressMarker reports whether a git-dir entry is an in-progress
// operation's marker.
func isInProgressMarker(name string) bool {
	for _, m := range gitDirInProgress {
		if m.entry == name {
			return true
		}
	}
	return false
}

// InProgressOperations lists the git operations stopped midway in the
// git dir gitDir, each once, in the order git's status reports them.
func InProgressOperations(fs afero.Fs, gitDir string) []GitOperation {
	var ops []GitOperation
	seen := map[string]bool{}
	for _, m := range gitDirInProgress {
		if ok, _ := afero.Exists(fs, filepath.Join(gitDir, m.entry)); !ok {
			continue
		}
		op := m.op(gitDir, fs)
		// A stopped pick of a sequence leaves both CHERRY_PICK_HEAD and
		// sequencer/: one operation, named by the more specific marker.
		if m.entry == "sequencer" && (seen[opCherryPick.Name] || seen[opRevert.Name]) {
			continue
		}
		if seen[op.Name] {
			continue
		}
		seen[op.Name] = true
		ops = append(ops, op)
	}
	return ops
}

// InProgressError is the refusal of a bare conversion while a git
// operation is in progress.
type InProgressError struct {
	Ops []GitOperation
	// Worktree is the linked worktree the operations are paused in;
	// empty for the main worktree.
	Worktree string
}

func (e *InProgressError) Error() string {
	names := make([]string, len(e.Ops))
	for i, op := range e.Ops {
		names[i] = op.Name
	}
	where, verb, it := "", "is", "it"
	if len(names) > 1 {
		verb, it = "are", "them"
	}
	if e.Worktree != "" {
		where = " in the linked worktree " + e.Worktree
	}
	return fmt.Sprintf("a %s %s in progress%s; converting would abandon %s", strings.Join(names, " and a "), verb, where, it)
}

// Hints returns one hint per operation, then to run retry, the caller's
// command line for the refused init, again.
func (e *InProgressError) Hints(retry string) []string {
	hints := make([]string, 0, len(e.Ops)+1)
	for _, op := range e.Ops {
		if e.Worktree != "" {
			hints = append(hints, op.hintIn(e.Worktree))
			continue
		}
		hints = append(hints, op.Hint())
	}
	return append(hints, "then run "+retry+" again")
}

// CheckNoOperationInProgress refuses a bare conversion of repoPath while
// a merge, rebase, am, cherry-pick, revert or bisect is in progress
// there, or in one of its linked worktrees. The operation's state
// belongs to the git dir the conversion replaces, so converting would
// silently abandon it; --force does not override this, since it
// consents to carrying files, not to losing a paused operation. The main
// worktree is checked first, then the linked ones by id; the first with
// an operation is reported.
func CheckNoOperationInProgress(fs afero.Fs, repoPath string) error {
	if ops := InProgressOperations(fs, filepath.Join(repoPath, ".git")); len(ops) > 0 {
		return &InProgressError{Ops: ops}
	}
	for _, a := range liveLinkedAdmins(fs, repoPath) {
		if ops := InProgressOperations(fs, a.dir); len(ops) > 0 {
			return &InProgressError{Ops: ops, Worktree: a.path()}
		}
	}
	return nil
}
