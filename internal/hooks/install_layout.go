package hooks

import (
	"path/filepath"
	"strings"

	"github.com/spf13/afero"

	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// A mirror writes where hop.dataLayout, as the worktree's repository
// resolves it, puts the hopspace hooks dir. Another hub of the same
// repository can resolve it elsewhere (its own hop.dataLayout, or none
// where this one has one). The hooks then land where that hub never
// looks, and doctor --fix will not move them while the hubs disagree.
// The mirror still writes where this worktree resolves: that is where
// its own hook lookup reads, and picking another hub's location would be
// a guess. It warns first, naming each hub's value and how to align them,
// as doctor does.

// warnLayoutSplit warns when a hub of the repository repoID that state
// records resolves hop.dataLayout to another hopspace than the worktree
// does. It runs git config for each hub; call it outside any lock.
func warnLayoutSplit(fs afero.Fs, worktreePath, repoID string, ref hop.RepoRef) {
	hubs := append([]string{worktreePath}, otherHubs(fs, worktreePath, repoID)...)
	if len(hubs) < 2 {
		return
	}
	layouts := hop.ResolveHubLayouts(hop.GetGitHopDataHome(), ref, hubs)
	if _, ok := hop.AgreedHopspace(layouts); ok {
		return
	}
	output.Warn("hubs of %s/%s resolve hop.dataLayout to different hopspaces (%s); mirroring committed hooks to %s, where this worktree's hop.dataLayout puts them",
		ref.Org, ref.Repo, hop.LayoutSplitSummary(layouts), filepath.Join(layouts[0].Path, "hooks"))
	output.Hint("%s", hop.LayoutAlignmentHint(layouts))
}

// otherHubs returns the hubs state records for repoID whose directories
// exist, but the one worktreePath lies in: the worktree speaks for it.
// None when state cannot be read.
func otherHubs(fs afero.Fs, worktreePath, repoID string) []string {
	st, err := state.LoadState(fs)
	if err != nil {
		return nil
	}
	repo := st.Repositories[repoID]
	if repo == nil {
		return nil
	}
	var hubs []string
	for _, h := range repo.Hubs {
		if h == nil || h.Path == "" || within(worktreePath, h.Path) {
			continue
		}
		if ok, _ := afero.DirExists(fs, h.Path); ok {
			hubs = append(hubs, h.Path)
		}
	}
	return hubs
}

// within reports whether path is dir or lies inside it.
func within(path, dir string) bool {
	path, dir = state.ResolvePath(path), state.ResolvePath(dir)
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}
