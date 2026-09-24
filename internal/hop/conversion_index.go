package hop

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A bare conversion checks the default branch out into a fresh worktree,
// whose index is HEAD's, then moves the old working tree over it. That
// carries file content but not the index: without more, every staged
// change would come back unstaged, and a file deleted from the old
// working tree would come back from the checkout. indexCarry records
// what the old index and working tree held, before the move, so
// restoreIndex can rebuild the same staged/unstaged split after it.
type indexCarry struct {
	// staged is the staged diff (HEAD to index), a binary patch that
	// `git apply --cached` replays onto the new index: adds, deletions,
	// renames, mode changes, binary files and gitlinks alike.
	staged []byte
	// removed are the paths HEAD tracks that the old working tree lacks,
	// deleted from the fresh checkout again; prune the directories that
	// held them and did not exist either.
	removed, prune []string
	// intentToAdd are `git add -N` entries, which no patch expresses.
	intentToAdd []string
	// unmerged paths keep their content, conflict markers included, but
	// not their stages; they come back unstaged.
	unmerged []string
}

// diffArgs pins every diff option user config can change, so the output
// is a patch `git apply` reads back and a NUL-separated list of names
// whatever diff.noprefix, diff.relative, diff.external, color.diff or
// diff.context say.
var diffArgs = []string{
	"-c", "diff.suppressBlankEmpty=false",
	"diff", "--no-color", "--no-ext-diff", "--no-textconv", "--no-relative",
	"--no-renames", "--src-prefix=a/", "--dst-prefix=b/", "--unified=3",
	"--submodule=short", "--ita-invisible-in-index",
}

// gitDiff runs `git -C repo diff <args>` into a file under scratch and
// returns its raw bytes: Run trims its output, which would cut a patch's
// last newline or a name's edge whitespace.
func (c *Converter) gitDiff(repo, scratch string, args ...string) ([]byte, error) {
	out := filepath.Join(scratch, "hop-diff.out")
	defer os.Remove(out)
	full := append([]string{"-C", repo}, diffArgs...)
	full = append(full, "--output="+out)
	if _, err := c.git.Run("git", append(full, args...)...); err != nil {
		return nil, err
	}
	return os.ReadFile(out)
}

func splitNUL(b []byte) []string {
	var names []string
	for _, n := range bytes.Split(b, []byte{0}) {
		if len(n) > 0 {
			names = append(names, string(n))
		}
	}
	return names
}

// planIndexCarry reads the old repository's index and working tree. It
// must run before the working tree moves: which files it lacks is only
// visible there. scratch is a directory for git's output files.
func (c *Converter) planIndexCarry(repoPath, scratch string) (*indexCarry, error) {
	ic := &indexCarry{}
	var err error
	if ic.staged, err = c.gitDiff(repoPath, scratch, "--cached", "--binary"); err != nil {
		return nil, fmt.Errorf("failed to read the staged changes: %w", err)
	}
	names, err := c.gitDiff(repoPath, scratch, "HEAD", "--name-only", "-z", "--diff-filter=D")
	if err != nil {
		return nil, fmt.Errorf("failed to list the deleted files: %w", err)
	}
	pruned := map[string]bool{}
	for _, name := range splitNUL(names) {
		// Deleted from the index but present on disk is an untracked
		// file (git rm --cached): the move carries it.
		if _, err := c.fs.Stat(filepath.Join(repoPath, name)); err == nil {
			continue
		}
		ic.removed = append(ic.removed, name)
		for dir := filepath.Dir(name); dir != "." && !pruned[dir]; dir = filepath.Dir(dir) {
			if _, err := c.fs.Stat(filepath.Join(repoPath, dir)); err == nil {
				break
			}
			pruned[dir] = true
			ic.prune = append(ic.prune, dir)
		}
	}
	// Deepest first, so a directory is empty by the time it is tried.
	sort.Slice(ic.prune, func(i, j int) bool { return len(ic.prune[i]) > len(ic.prune[j]) })

	if names, err = c.gitDiff(repoPath, scratch, "--name-only", "-z", "--diff-filter=A"); err != nil {
		return nil, fmt.Errorf("failed to list the intent-to-add files: %w", err)
	}
	ic.intentToAdd = splitNUL(names)
	if names, err = c.gitDiff(repoPath, scratch, "--cached", "--name-only", "-z", "--diff-filter=U"); err != nil {
		return nil, fmt.Errorf("failed to list the unmerged files: %w", err)
	}
	ic.unmerged = splitNUL(names)
	return ic, nil
}

// restoreIndex rebuilds the old index in the worktree at worktreePath,
// once the old working tree has moved in. It never fails the conversion:
// what it cannot restore stays in the working tree, unstaged, and is
// named in a warning.
func (c *Converter) restoreIndex(ic *indexCarry, worktreePath, scratch string) []string {
	var warnings []string
	for _, name := range ic.removed {
		if err := c.fs.RemoveAll(filepath.Join(worktreePath, name)); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: deleted before the conversion, but could not be removed from the worktree: %v", name, err))
		}
	}
	for _, dir := range ic.prune {
		_ = c.fs.Remove(filepath.Join(worktreePath, dir)) // fails, rightly, unless empty
	}

	if len(ic.staged) > 0 {
		if err := c.applyCached(ic.staged, worktreePath, scratch); err != nil {
			warnings = append(warnings, fmt.Sprintf("staged changes could not be restored and are left unstaged in the worktree: %v", err))
		}
	}
	if len(ic.intentToAdd) > 0 {
		args := append([]string{"-C", worktreePath, "add", "--intent-to-add", "--"}, ic.intentToAdd...)
		if _, err := c.git.Run("git", args...); err != nil {
			warnings = append(warnings, fmt.Sprintf("intent-to-add files left untracked (%s): %v", strings.Join(ic.intentToAdd, ", "), err))
		}
	}
	for _, name := range ic.unmerged {
		warnings = append(warnings, fmt.Sprintf("%s: unmerged; its content, conflict markers included, is left unstaged, without the conflict's stages", name))
	}
	return warnings
}

func (c *Converter) applyCached(patch []byte, worktreePath, scratch string) error {
	file := filepath.Join(scratch, "hop-staged.patch")
	if err := os.WriteFile(file, patch, 0o600); err != nil {
		return err
	}
	defer os.Remove(file)
	// --whitespace=nowarn: apply.whitespace=fix or error must not alter
	// or refuse content the user staged as it is.
	_, err := c.git.Run("git", "-C", worktreePath, "apply", "--cached", "--binary", "--whitespace=nowarn", file)
	return err
}
