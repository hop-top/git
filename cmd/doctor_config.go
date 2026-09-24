package cmd

import (
	"hop.top/git/internal/config"
	"hop.top/git/internal/output"
)

// checkConfig reports --global hop.* keys the zero-value global.json
// migration wrote and the user never set (see
// config.GlobalLoader.MigrationDebris), and retired settings git-hop wrote
// on its own (see config.GlobalLoader.StaleRetiredSettings). Under --fix it
// unsets both. The repair runs only here, never on its own: it edits the
// user's global git config.
func checkConfig(l *config.GlobalLoader, opts doctorOpts, r *doctorReport) {
	output.Info("\n=== Checking Config ===")
	reported := checkMigrationDebris(l, opts, r)
	checkStaleRetired(l, opts, r, reported)
}

// checkMigrationDebris reports and, under --fix, unsets the migration
// leftovers. It returns the keys it reported.
func checkMigrationDebris(l *config.GlobalLoader, opts doctorOpts, r *doctorReport) map[string]bool {
	debris, err := l.MigrationDebris()
	if err != nil {
		output.Warn("Could not check for migration leftovers: %v", err)
		r.record(doctorKindWarning, doctorCheckConfig, "global.json.bak", "could not check for migration leftovers: %v", err)
		return nil
	}
	if len(debris) == 0 {
		output.Info("No migration leftovers in git config --global")
		return nil
	}

	reported := map[string]bool{}
	for _, e := range debris {
		reported[e.Key] = true
		output.Error("%s = %q was written by the global.json migration and shadows the default", e.Key, e.Value)
		r.issue(doctorCheckConfig, e.Key, "%q was written by the global.json migration and shadows the default; run 'git hop doctor --fix' to unset it", e.Value)
		if !opts.fix {
			continue
		}
		if !opts.mutating() {
			output.Info("[dry-run] Would unset %s from git config --global", e.Key)
			r.repaired(opts, doctorCheckConfig, e.Key, "unset from git config --global")
			continue
		}
		if err := l.RemoveMigrationDebris([]config.DebrisEntry{e}); err != nil {
			output.Error("Failed to unset %s: %v", e.Key, err)
			r.failed(doctorCheckConfig, e.Key, "unset from git config --global: %v", err)
			continue
		}
		output.Info("Unset %s from git config --global", e.Key)
		r.repaired(opts, doctorCheckConfig, e.Key, "unset from git config --global")
	}
	if !opts.fix {
		output.Info("  Run 'git hop doctor --fix' to unset them")
	}
	return reported
}

// checkStaleRetired warns about --global keys of retired settings git-hop
// wrote on its own and, under --fix, unsets them. Nothing reads them, so
// they are warnings and leave the exit status alone. Keys already reported
// as migration debris are skipped: that check owns them.
func checkStaleRetired(l *config.GlobalLoader, opts doctorOpts, r *doctorReport, skip map[string]bool) {
	stale, err := l.StaleRetiredSettings()
	if err != nil {
		output.Warn("Could not check for retired settings: %v", err)
		r.record(doctorKindWarning, doctorCheckConfig, "git config --global", "could not check for retired settings: %v", err)
		return
	}

	for _, s := range stale {
		if skip[s.Key] {
			continue
		}
		const msg = "retired setting %s is ignored; the setting is now %s"
		output.Warn(msg, s.Key, s.Replacement)
		r.record(doctorKindWarning, doctorCheckConfig, s.Key, msg+"; run 'git hop doctor --fix' to unset it", s.Key, s.Replacement)
		if !opts.fix {
			output.Hint("to turn %s on, run 'git config --global %s true'\n"+
				"run 'git hop doctor --fix' to unset %s", s.Replacement, s.Replacement, s.Key)
			continue
		}
		if !opts.mutating() {
			output.Info("[dry-run] Would unset %s from git config --global", s.Key)
			r.repaired(opts, doctorCheckConfig, s.Key, "unset from git config --global")
			continue
		}
		if err := l.RemoveStaleRetiredSetting(s.Key); err != nil {
			output.Error("Failed to unset %s: %v", s.Key, err)
			r.failed(doctorCheckConfig, s.Key, "unset from git config --global: %v", err)
			continue
		}
		output.Info("Unset %s from git config --global", s.Key)
		r.repaired(opts, doctorCheckConfig, s.Key, "unset from git config --global")
	}
}
