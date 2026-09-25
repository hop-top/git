package cmd

import (
	"bytes"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/repoid"
	"hop.top/git/internal/state"
)

// legacyHooksHost is the host every hopspace hooks dir was mirrored under
// before hop.dataLayout, whatever the origin.
const legacyHooksHost = hop.LegacyHooksHost

// checkDataLayout warns about an invalid hop.dataLayout, about hopspaces
// of --global hubs left at another layout's path, and about hopspace
// hooks left where releases before hop.dataLayout mirrored them. It runs
// before the hub check, which would otherwise create a hopspace where one
// moved here belongs.
func checkDataLayout(fs afero.Fs, cwd string, opts doctorOpts, r *doctorReport) {
	hubPath, _ := hop.FindHub(fs, cwd)
	checkDataLayoutSetting(hubPath, r)
	claimed := checkMisplacedHopspaces(fs, hubPath, opts, r)
	checkLegacyHooksDirs(fs, opts, r, claimed)
}

// checkDataLayoutSetting reports a hop.dataLayout git-hop cannot use: the
// --global value, which every repository without its own falls back on,
// and the value in effect for the hub at hubPath ("" outside a hub) when
// another scope (the repository, `git -c`) sets it. Each warning names
// the scope and the layout used instead.
func checkDataLayoutSetting(hubPath string, r *doctorReport) {
	key := config.KeyDataLayout
	if raw, err := config.NewGlobalGitConfig().GetString(key); err == nil {
		if verr := hop.ValidateDataLayout(raw); verr != nil {
			warnInvalidDataLayout(r, "global", raw, verr, config.Default(key))
		}
	}
	if s := hop.ResolveDataLayout(hubPath); s.Err != nil && s.Scope != "global" {
		warnInvalidDataLayout(r, s.Scope, s.Raw, s.Err, s.Layout)
	}
}

func warnInvalidDataLayout(r *doctorReport, scope, raw string, err error, using string) {
	key := config.KeyDataLayout
	output.Warn("%s %q (%s config) is invalid (%v); using %s", key, raw, scope, err, using)
	r.record(doctorKindWarning, doctorCheckConfig, key, "%q (%s config) is invalid (%v); using %s", raw, scope, err, using)
}

// checkLegacyHooksDirs finds <data>/github.com/<org>/<repo>/hooks dirs that
// are not the hop.dataLayout hooks dir of that repository. Their hooks
// still fire (hook lookup reads them after the new location), so each is
// a warning. --fix moves one to the new location by rename, only when
// nothing is there yet; with hooks at both locations it changes nothing.
// Nothing is ever deleted or overwritten. A hooks dir inside a hopspace
// checkMisplacedHopspaces reported (skip) moves with that hopspace, so it
// is left to that check.
func checkLegacyHooksDirs(fs afero.Fs, opts doctorOpts, r *doctorReport, skip map[string]bool) {
	dataHome := hop.GetGitHopDataHome()
	repos := stateRepos(fs)
	for _, legacy := range findLegacyHooksDirs(fs, dataHome) {
		if skip[filepath.Clean(filepath.Dir(legacy))] {
			continue
		}
		org := filepath.Base(filepath.Dir(filepath.Dir(legacy)))
		repo := filepath.Base(filepath.Dir(legacy))
		var uri, hubDir string
		if st := legacyHooksRepo(repos, org, repo); st != nil {
			uri = st.URI
			if len(st.Hubs) > 0 && st.Hubs[0] != nil {
				hubDir = st.Hubs[0].Path
			}
		}
		newDir := hop.HopspaceHooksDir(hop.NewRepoRef(uri, org, repo).In(hubDir))
		if filepath.Clean(newDir) == filepath.Clean(legacy) {
			continue
		}
		reportLegacyHooksDir(fs, opts, r, legacy, newDir)
	}
}

func reportLegacyHooksDir(fs afero.Fs, opts doctorOpts, r *doctorReport, legacy, newDir string) {
	if exists, _ := afero.Exists(fs, newDir); exists {
		detail := "same-named hooks match"
		if hookDirsConflict(fs, legacy, newDir) {
			detail = "different content"
		}
		msg := "hooks at old location %s and at %s (%s); the old ones fire only where %s lacks the hook. Nothing moved: merge them by hand"
		output.Warn(msg, legacy, newDir, detail, newDir)
		r.record(doctorKindWarning, doctorCheckHopspace, legacy, msg, legacy, newDir, detail, newDir)
		return
	}

	output.Warn("hooks at old location %s; they belong at %s", legacy, newDir)
	r.record(doctorKindWarning, doctorCheckHopspace, legacy,
		"hooks at old location; they belong at %s: run 'git hop doctor --fix' to move them", newDir)

	switch {
	case !opts.fix:
		output.Hint("run 'git hop doctor --fix' to move them to %s", newDir)
	case !opts.mutating():
		output.Info("[dry-run] Would move %s -> %s", legacy, newDir)
		r.repaired(opts, doctorCheckHopspace, legacy, "move to %s", newDir)
	default:
		present := func() bool { ok, _ := afero.DirExists(fs, legacy); return ok }
		if err := moveUnderHopspaceLocks(fs, filepath.Dir(legacy), legacy, newDir, present); err != nil {
			output.Error("Failed to move %s: %v", legacy, err)
			r.failed(doctorCheckHopspace, legacy, "move to %s: %v", newDir, err)
			return
		}
		output.Info("Moved %s -> %s", legacy, newDir)
		r.repaired(opts, doctorCheckHopspace, legacy, "move to %s", newDir)
	}
}

type existsError struct{ path string }

func (e *existsError) Error() string { return e.path + " already exists" }

// findLegacyHooksDirs lists <dataHome>/github.com/<org>/<repo>/hooks dirs,
// sorted.
func findLegacyHooksDirs(fs afero.Fs, dataHome string) []string {
	root := filepath.Join(dataHome, legacyHooksHost)
	var dirs []string
	for _, org := range subdirs(fs, root) {
		for _, repo := range subdirs(fs, filepath.Join(root, org)) {
			hooks := filepath.Join(root, org, repo, "hooks")
			if ok, _ := afero.DirExists(fs, hooks); ok {
				dirs = append(dirs, hooks)
			}
		}
	}
	sort.Strings(dirs)
	return dirs
}

func subdirs(fs afero.Fs, dir string) []string {
	entries, err := afero.ReadDir(fs, dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	return names
}

// hookDirsConflict reports whether a file in a has a same-named file in b
// with different content.
func hookDirsConflict(fs afero.Fs, a, b string) bool {
	entries, err := afero.ReadDir(fs, a)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		other, err := afero.ReadFile(fs, filepath.Join(b, e.Name()))
		if err != nil {
			continue
		}
		mine, err := afero.ReadFile(fs, filepath.Join(a, e.Name()))
		if err != nil || !bytes.Equal(mine, other) {
			return true
		}
	}
	return false
}

// legacyHooksRepo returns the repository whose hooks releases before
// hop.dataLayout kept in <data>/github.com/<org>/<repo>: those releases
// keyed every repository github.com/<org>/<repo>, and state now keys it by
// its origin's host. The github.com entry wins when there is one;
// otherwise the one entry of org/repo on another host. nil when there is
// none, or several on other hosts, in which case the host falls back to
// hop.gitDomain and hop.dataLayout to the --global value.
func legacyHooksRepo(repos map[string]*state.RepositoryState, org, repo string) *state.RepositoryState {
	if st, ok := repos[legacyHooksHost+"/"+org+"/"+repo]; ok {
		return st
	}
	var found *state.RepositoryState
	n := 0
	for id, st := range repos {
		if _, o, r, ok := repoid.Split(id); ok && o == org && r == repo {
			found, n = st, n+1
		}
	}
	if n != 1 {
		return nil
	}
	return found
}

// stateRepos returns the repositories state records, by repo ID; empty
// when state cannot be read, in which case the host falls back to
// hop.gitDomain and hop.dataLayout to the --global value.
func stateRepos(fs afero.Fs) map[string]*state.RepositoryState {
	st, err := state.LoadState(fs)
	if err != nil || st.Repositories == nil {
		return map[string]*state.RepositoryState{}
	}
	return st.Repositories
}
