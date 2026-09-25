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
	"hop.top/git/internal/state"
)

// legacyHooksHost is the host every hopspace hooks dir was mirrored under
// before hop.dataLayout: the fixed host of the repo ID, whatever the origin.
const legacyHooksHost = "github.com"

// checkDataLayout warns about an invalid hop.dataLayout and about hopspace
// hooks left where releases before hop.dataLayout mirrored them.
func checkDataLayout(fs afero.Fs, opts doctorOpts, r *doctorReport) {
	checkDataLayoutSetting(r)
	checkLegacyHooksDirs(fs, opts, r)
}

// checkDataLayoutSetting reports a --global hop.dataLayout git-hop cannot
// use; hop.DataLayout falls back to the default for it.
func checkDataLayoutSetting(r *doctorReport) {
	key := config.KeyDataLayout
	raw, err := config.NewGlobalGitConfig().GetString(key)
	if err != nil {
		return
	}
	if verr := hop.ValidateDataLayout(raw); verr != nil {
		def := config.Default(key)
		output.Warn("%s %q is invalid (%v); using %s", key, raw, verr, def)
		r.record(doctorKindWarning, doctorCheckConfig, key, "%q is invalid (%v); using %s", raw, verr, def)
	}
}

// checkLegacyHooksDirs finds <data>/github.com/<org>/<repo>/hooks dirs that
// are not the hop.dataLayout hooks dir of that repository. Their hooks
// still fire (hook lookup reads them after the new location), so each is
// a warning. --fix moves one to the new location by rename, only when
// nothing is there yet; with hooks at both locations it changes nothing.
// Nothing is ever deleted or overwritten.
func checkLegacyHooksDirs(fs afero.Fs, opts doctorOpts, r *doctorReport) {
	dataHome := hop.GetGitHopDataHome()
	uris := stateRepoURIs(fs)
	for _, legacy := range findLegacyHooksDirs(fs, dataHome) {
		org := filepath.Base(filepath.Dir(filepath.Dir(legacy)))
		repo := filepath.Base(filepath.Dir(legacy))
		repoID := legacyHooksHost + "/" + org + "/" + repo
		newDir := hop.HopspaceHooksDir(hop.NewRepoRef(uris[repoID], org, repo))
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
		if err := moveHooksDir(fs, legacy, newDir); err != nil {
			output.Error("Failed to move %s: %v", legacy, err)
			r.failed(doctorCheckHopspace, legacy, "move to %s: %v", newDir, err)
			return
		}
		output.Info("Moved %s -> %s", legacy, newDir)
		r.repaired(opts, doctorCheckHopspace, legacy, "move to %s", newDir)
	}
}

// moveHooksDir renames legacy to newDir, creating newDir's parent. It
// refuses when newDir exists: rename would replace an empty directory.
func moveHooksDir(fs afero.Fs, legacy, newDir string) error {
	if err := fs.MkdirAll(filepath.Dir(newDir), 0o755); err != nil {
		return err
	}
	if exists, _ := afero.Exists(fs, newDir); exists {
		return &existsError{path: newDir}
	}
	return fs.Rename(legacy, newDir)
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

// stateRepoURIs maps each repo ID in state to its origin URL; empty when
// state cannot be read, in which case the host falls back to hop.gitDomain.
func stateRepoURIs(fs afero.Fs) map[string]string {
	uris := map[string]string{}
	st, err := state.LoadState(fs)
	if err != nil {
		return uris
	}
	for id, repo := range st.Repositories {
		if repo != nil {
			uris[id] = repo.URI
		}
	}
	return uris
}
