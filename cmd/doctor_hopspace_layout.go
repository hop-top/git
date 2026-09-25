package cmd

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
	"hop.top/git/internal/state"
)

// hostDataLayout is the built-in alternative to the default hop.dataLayout.
const hostDataLayout = "{host}/{org}/{repo}"

// hopspaceMarkers are the entries that make a data-home directory hold a
// repository's hopspace data.
var hopspaceMarkers = []string{"hop.json", "ports.json", "volumes.json", "deps", "hooks"}

// layoutRepo is one repository with --global hubs: the hubs, and every
// worktree known for it, which a move must not break.
type layoutRepo struct {
	ref       hop.RepoRef // without Dir
	hubs      []string
	worktrees []string
}

// checkMisplacedHopspaces finds the data-home hopspaces of --global hubs
// left at a path another hop.dataLayout builds (the default, {host}/{org}/
// {repo}, or the --global value) while the layout in effect for the hub
// puts them elsewhere. Each is a warning. --fix moves one to its current
// path by rename, only when nothing is there and nothing would break;
// with data at both paths it changes nothing. Nothing is ever deleted or
// overwritten, and emptied parent directories stay.
//
// It returns the directories it reported, which the hooks-dir check then
// leaves alone.
func checkMisplacedHopspaces(fs afero.Fs, hubPath string, opts doctorOpts, r *doctorReport) map[string]bool {
	claimed := map[string]bool{}
	dataHome := hop.GetGitHopDataHome()
	repos := globalLayoutRepos(fs, hubPath)
	keys := make([]string, 0, len(repos))
	for k := range repos {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		repo := repos[k]
		current, ok := agreedHopspacePath(dataHome, repo, r)
		if !ok {
			continue
		}
		for _, alt := range alternateHopspacePaths(dataHome, repo.ref, current) {
			markers := presentMarkers(fs, alt)
			if len(markers) == 0 || ownedByLegacyHooksCheck(alt, repo.ref, markers) {
				continue
			}
			claimed[alt] = true
			reportMisplacedHopspace(fs, opts, r, repo, alt, current)
		}
	}
	return claimed
}

// globalLayoutRepos groups the --global hubs state records, plus the hub
// at hubPath, by repository. A hub's own hop.json decides whether it is
// global; one whose hop.json cannot be read falls back on the mode state
// recorded. A hub whose directory is gone is skipped: its config, and so
// its layout, cannot be read.
func globalLayoutRepos(fs afero.Fs, hubPath string) map[string]*layoutRepo {
	repos := map[string]*layoutRepo{}
	seen := map[string]bool{}
	add := func(path string, ref hop.RepoRef, stateMode string) {
		key := state.ResolvePath(path)
		if path == "" || seen[key] {
			return
		}
		seen[key] = true
		if ok, _ := afero.DirExists(fs, path); !ok {
			return
		}
		var worktrees []string
		if hub, err := hop.LoadHub(fs, path); err == nil {
			if hub.Config.Repo.Mode != config.RepoModeGlobal {
				return
			}
			ref = hop.RepoRefFor("", hub.Config.Repo)
			for _, wt := range hub.WorktreePaths() {
				worktrees = append(worktrees, wt)
			}
		} else if stateMode != state.HubModeGlobal {
			return
		}
		if ref.Org == "" || ref.Repo == "" {
			return
		}
		repo := repoEntry(repos, ref)
		repo.hubs = append(repo.hubs, path)
		repo.worktrees = append(repo.worktrees, worktrees...)
	}

	if st, err := state.LoadState(fs); err == nil {
		ids := make([]string, 0, len(st.Repositories))
		for id := range st.Repositories {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			rs := st.Repositories[id]
			if rs == nil {
				continue
			}
			ref := hop.NewRepoRef(rs.URI, rs.Org, rs.Repo)
			for _, h := range rs.Hubs {
				if h != nil {
					add(h.Path, ref, h.Mode)
				}
			}
			if repo, ok := repos[repoKey(ref)]; ok {
				for _, wt := range rs.Worktrees {
					if wt != nil {
						repo.worktrees = append(repo.worktrees, wt.Path)
					}
				}
			}
		}
	}
	add(hubPath, hop.RepoRef{}, "")
	return repos
}

func repoKey(ref hop.RepoRef) string {
	return ref.Host + "/" + ref.Org + "/" + ref.Repo
}

func repoEntry(repos map[string]*layoutRepo, ref hop.RepoRef) *layoutRepo {
	key := repoKey(ref)
	if repos[key] == nil {
		ref.Dir = ""
		repos[key] = &layoutRepo{ref: ref}
	}
	return repos[key]
}

// agreedHopspacePath returns where hop.dataLayout, as each hub of repo
// resolves it, puts the repository's hopspace. Hubs that resolve it to
// different paths would pull one hopspace two ways: that is reported and
// ok is false, so nothing moves.
func agreedHopspacePath(dataHome string, repo *layoutRepo, r *doctorReport) (string, bool) {
	byPath := map[string][]string{}
	var paths []string
	for _, hub := range repo.hubs {
		p := filepath.Clean(hop.GetHopspacePath(dataHome, repo.ref.In(hub)))
		if byPath[p] == nil {
			paths = append(paths, p)
		}
		byPath[p] = append(byPath[p], hub)
	}
	if len(paths) == 1 {
		return paths[0], true
	}
	var parts []string
	for _, p := range paths {
		parts = append(parts, fmt.Sprintf("%s -> %s", strings.Join(byPath[p], ", "), p))
	}
	name := repo.ref.Org + "/" + repo.ref.Repo
	msg := "hubs of %s resolve hop.dataLayout to different hopspaces (%s); nothing moved: give them the same hop.dataLayout"
	output.Warn(msg, name, strings.Join(parts, "; "))
	r.record(doctorKindWarning, doctorCheckHopspace, name, msg, name, strings.Join(parts, "; "))
	return "", false
}

// alternateHopspacePaths returns the paths the other known layouts build
// for ref, current excluded, sorted and without duplicates.
func alternateHopspacePaths(dataHome string, ref hop.RepoRef, current string) []string {
	layouts := []string{config.Default(config.KeyDataLayout), hostDataLayout, hop.DataLayout()}
	seen := map[string]bool{filepath.Clean(current): true}
	var paths []string
	for _, layout := range layouts {
		p := filepath.Clean(filepath.Join(dataHome, hop.ExpandDataLayout(layout, ref)))
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return paths
}

// presentMarkers returns the hopspace entries dir holds.
func presentMarkers(fs afero.Fs, dir string) []string {
	var found []string
	for _, m := range hopspaceMarkers {
		if ok, _ := afero.Exists(fs, filepath.Join(dir, m)); ok {
			found = append(found, m)
		}
	}
	return found
}

// ownedByLegacyHooksCheck reports whether dir holds nothing but the hooks
// dir releases before hop.dataLayout mirrored to: checkLegacyHooksDirs
// moves that one into the hopspace, whatever else is there.
func ownedByLegacyHooksCheck(dir string, ref hop.RepoRef, markers []string) bool {
	if len(markers) != 1 || markers[0] != "hooks" {
		return false
	}
	legacy := hop.LegacyHooksDir(legacyHooksHost + "/" + ref.Org + "/" + ref.Repo)
	return legacy != "" && filepath.Clean(filepath.Dir(legacy)) == filepath.Clean(dir)
}

func reportMisplacedHopspace(fs afero.Fs, opts doctorOpts, r *doctorReport, repo *layoutRepo, alt, current string) {
	if exists, _ := afero.Exists(fs, current); exists {
		msg := "hopspace data at %s and at %s, where hop.dataLayout puts it; nothing moved: merge them by hand"
		output.Warn(msg, alt, current)
		r.record(doctorKindWarning, doctorCheckHopspace, alt, msg, alt, current)
		r.markMisplaced(current)
		return
	}
	if reason := hopspaceMoveBlocker(fs, alt, repo.worktrees); reason != "" {
		msg := "hopspace at %s belongs at %s (hop.dataLayout), but %s; nothing moved"
		output.Warn(msg, alt, current, reason)
		r.record(doctorKindWarning, doctorCheckHopspace, alt, msg, alt, current, reason)
		r.markMisplaced(current)
		return
	}

	output.Warn("hopspace at %s is not where hop.dataLayout puts it: %s", alt, current)
	r.record(doctorKindWarning, doctorCheckHopspace, alt,
		"hopspace left at another hop.dataLayout location; it belongs at %s: run 'git hop doctor --fix' to move it", current)

	switch {
	case !opts.fix:
		output.Hint("run 'git hop doctor --fix' to move it to %s", current)
		r.markMisplaced(current)
	case !opts.mutating():
		output.Info("[dry-run] Would move %s -> %s", alt, current)
		r.repaired(opts, doctorCheckHopspace, alt, "move to %s", current)
		previewHopspaceMove(fs, alt, current)
	default:
		if err := moveDirNoClobber(fs, alt, current); err != nil {
			output.Error("Failed to move %s: %v", alt, err)
			r.failed(doctorCheckHopspace, alt, "move to %s: %v", current, err)
			r.markMisplaced(current)
			return
		}
		output.Info("Moved %s -> %s", alt, current)
		r.repaired(opts, doctorCheckHopspace, alt, "move to %s", current)
	}
}

// hopspaceMoveBlocker says why renaming dir would break a worktree, ""
// when it would not: a worktree inside it would lose its git link, and a
// worktree symlink into it (a deps store link) would dangle.
func hopspaceMoveBlocker(fs afero.Fs, dir string, worktrees []string) string {
	var roots []string
	seen := map[string]bool{}
	for _, wt := range worktrees {
		if isWithinDir(wt, dir) {
			return fmt.Sprintf("worktree %s lies inside it", wt)
		}
		// Hub and state list the same worktree; walk it once.
		if key := state.ResolvePath(wt); !seen[key] {
			seen[key] = true
			roots = append(roots, wt)
		}
	}
	links, err := services.LinksInto(fs, roots, dir)
	if err != nil {
		return fmt.Sprintf("its worktrees cannot be checked for links into it (%v)", err)
	}
	if len(links) > 0 {
		return fmt.Sprintf("%d worktree link(s) point into it, e.g. %s", len(links), links[0])
	}
	return ""
}

func isWithinDir(path, dir string) bool {
	path, dir = state.ResolvePath(path), state.ResolvePath(dir)
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// moveDirNoClobber renames from to to, creating to's parent. It refuses
// when to exists, checked right before the rename: rename would replace
// an empty directory.
func moveDirNoClobber(fs afero.Fs, from, to string) error {
	if err := fs.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	if exists, _ := afero.Exists(fs, to); exists {
		return &existsError{path: to}
	}
	return fs.Rename(from, to)
}

// previewHopspaceMove shows the checks after this one, on the scratch layer a
// --dry-run gives them, the hopspace a real run would have moved: its
// top-level files and directories appear at to. On any other filesystem
// it does nothing.
func previewHopspaceMove(fs afero.Fs, from, to string) {
	layer, ok := fs.(*afero.CopyOnWriteFs)
	if !ok {
		return
	}
	_ = layer.MkdirAll(to, 0o755)
	entries, err := afero.ReadDir(layer, from)
	if err != nil {
		return
	}
	for _, e := range entries {
		src, dst := filepath.Join(from, e.Name()), filepath.Join(to, e.Name())
		switch {
		case e.IsDir():
			_ = layer.MkdirAll(dst, 0o755)
		case e.Mode().IsRegular():
			if data, err := afero.ReadFile(layer, src); err == nil {
				_ = afero.WriteFile(layer, dst, data, e.Mode().Perm())
			}
		}
	}
}
