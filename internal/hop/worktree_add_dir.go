package hop

import (
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
)

// worktreeAddDir is where `git worktree add` runs for the repository that
// the worktree at base belongs to: the bare repository itself, or, for a
// non-bare repository (a --regular hub), its main worktree, which is the
// hub.
//
// It matters because `git worktree add` (git 2.36+) copies the sparse
// checkout and config.worktree of the worktree it runs in into the new
// one. Run from base, every new worktree would inherit base's per-worktree
// settings; a bare repository has none to give.
//
// When git cannot tell, base is kept.
func worktreeAddDir(fs afero.Fs, g git.GitInterface, base string) string {
	p, ok := probeRepo(g, base)
	if !ok {
		return base
	}
	if p.bare {
		return p.gitDir
	}
	common := p.commonDir
	if isBareRepoRoot(fs, g, common) {
		return common
	}
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common)
	}
	return base
}

// pinStartPoint keeps a start-point meaning what it meant in base once
// `git worktree add` runs in addDir. A ref that names the same commit in
// both (a branch, a tag, a remote-tracking branch) is passed on as-is, so
// git's tracking rules still see it; one that differs (HEAD, @{-1}, a
// per-worktree ref) is only meaningful in base and is replaced by the
// commit it names there.
func pinStartPoint(g git.GitInterface, base, addDir, ref string) string {
	if ref == "" {
		return ref
	}
	commit := func(dir string) string {
		out, err := g.RevParse(dir, "--verify", "--quiet", ref+"^{commit}")
		if err != nil {
			return ""
		}
		return strings.TrimSpace(out)
	}
	inBase := commit(base)
	if inBase == "" || inBase == commit(addDir) {
		return ref
	}
	return inBase
}
