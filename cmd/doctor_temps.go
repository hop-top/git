package cmd

import (
	"errors"
	"path/filepath"

	"github.com/spf13/afero"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// checkStaleTemps warns about the temp files saves of state.json and of
// the current hub's hop.json left behind when their run died before
// renaming them into place, and under --fix removes them, with the sweep
// prune uses (see prune_temps.go). They take up space and nothing else:
// a warning, which leaves the exit status alone.
func checkStaleTemps(fs afero.Fs, hubPath string, opts doctorOpts, r *doctorReport) {
	found := checkTempsIn(doctorCheckState, state.GetStateHome(),
		func() ([]string, error) { return state.StaleTemps(fs) },
		func(dryRun bool) ([]string, error) { return state.SweepTemps(fs, dryRun) },
		opts, r)
	for _, dir := range hubTempDirs(fs, hubPath) {
		found = checkTempsIn(doctorCheckHub, dir,
			func() ([]string, error) { return hop.StaleHopJSONTemps(fs, dir) },
			func(dryRun bool) ([]string, error) { return hop.SweepHopJSONTemps(fs, dir, dryRun) },
			opts, r) || found
	}
	if found && !opts.fix {
		output.Hint("run 'git hop doctor --fix' to remove the stale temp files")
	}
}

// hubTempDirs are the directories holding the current hub's hop.json
// files: the hub's, and its hopspace's when that lies elsewhere. A
// directory without a hop.json is left out.
func hubTempDirs(fs afero.Fs, hubPath string) []string {
	if hubPath == "" {
		return nil
	}
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, dir := range []string{hubPath, hop.ResolveHopspacePath(hubPath, hub.Config.Repo)} {
		if len(dirs) > 0 && state.SamePath(dir, dirs[0]) {
			continue
		}
		if ok, _ := afero.Exists(fs, filepath.Join(dir, "hop.json")); ok {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// checkTempsIn reports the stale temp files list finds in dir under
// check, and under --fix removes them with sweep. It returns whether it
// found any.
func checkTempsIn(check, dir string, list func() ([]string, error), sweep func(dryRun bool) ([]string, error), opts doctorOpts, r *doctorReport) bool {
	stale, err := list()
	if err != nil {
		output.Warn("Could not check %s for stale temp files: %v", dir, err)
		r.record(doctorKindWarning, check, dir, "could not check for stale temp files: %v", err)
		return false
	}
	for _, path := range stale {
		output.Warn("stale temp file left by an interrupted save: %s", path)
		r.record(doctorKindWarning, check, path, "stale temp file left by an interrupted save; run 'git hop doctor --fix' to remove it")
	}
	if len(stale) == 0 || !opts.fix {
		return len(stale) > 0
	}

	removed, err := sweep(!opts.mutating())
	for _, path := range removed {
		if opts.planning() {
			output.Info("[dry-run] Would remove %s", path)
		} else {
			output.Info("Removed %s", path)
		}
		r.repaired(opts, check, path, "remove stale temp file")
	}
	switch {
	case errors.Is(err, state.ErrLocked), errors.Is(err, hop.ErrHopJSONLocked):
		// A save is running now: the files stay for a later run.
		output.Warn("stale temp files left in place: %v", err)
	case err != nil:
		output.Error("Failed to remove stale temp files: %v", err)
		r.failed(check, dir, "remove stale temp files: %v", err)
	}
	return true
}
