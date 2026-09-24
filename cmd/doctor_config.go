package cmd

import (
	"hop.top/git/internal/config"
	"hop.top/git/internal/output"
)

// checkConfig reports --global hop.* keys the zero-value global.json
// migration wrote and the user never set (see
// config.GlobalLoader.MigrationDebris), and under --fix unsets them. The
// repair runs only here, never on its own: it edits the user's global git
// config.
func checkConfig(l *config.GlobalLoader, opts doctorOpts, r *doctorReport) {
	output.Info("\n=== Checking Config ===")
	debris, err := l.MigrationDebris()
	if err != nil {
		output.Warn("Could not check for migration leftovers: %v", err)
		r.record(doctorKindWarning, doctorCheckConfig, "global.json.bak", "could not check for migration leftovers: %v", err)
		return
	}
	if len(debris) == 0 {
		output.Info("No migration leftovers in git config --global")
		return
	}

	for _, e := range debris {
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
}
