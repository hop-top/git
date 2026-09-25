package cmd

import (
	"fmt"
	"os"

	"hop.top/git/internal/config"
	"hop.top/git/internal/output"
)

// checkConfig reports a broken managers.json (checkManagersFile), a
// leftover config.json nothing reads (checkRetiredConfigFile), --global
// hop.* keys the zero-value global.json migration wrote and the user never
// set (see config.GlobalLoader.MigrationDebris), and keys of retired settings in
// --global and in the current hub's --local config (see
// config.ConfigScope.StaleRetiredSettings), and the hub's repository
// format (checkRepositoryFormat). Under --fix it unsets the keys and
// raises the format version.
// The repair runs only here, never on its own: it edits the user's git
// config. hubPath is "" outside a hub.
func checkConfig(l *config.GlobalLoader, hubPath string, opts doctorOpts, r *doctorReport) {
	output.Info("\n=== Checking Config ===")
	checkManagersFile(l, r)
	checkRetiredConfigFile(r)
	checkMigrationDebris(l, opts, r)

	scopes := []config.ConfigScope{l.GlobalScope()}
	if hubPath != "" {
		if s, ok := config.HubScope(hubPath); ok {
			scopes = append(scopes, s)
			checkRepositoryFormat(s, opts, r)
		}
	}
	found := 0
	for _, s := range scopes {
		found += checkStaleRetired(s, opts, r)
	}
	if found > 0 && !opts.fix {
		output.Hint("run 'git hop doctor --fix' to unset them")
	}
}

// checkManagersFile reports a managers.json that cannot be read or parsed.
// Every command skips such a file with a warning, so its package and
// environment managers stop applying until the user repairs it; --fix
// cannot guess what it should hold.
func checkManagersFile(l *config.GlobalLoader, r *doctorReport) {
	err := l.ManagersFileError()
	if err == nil {
		return
	}
	output.Error("%v; its package and environment managers are ignored", err)
	r.issue(doctorCheckConfig, config.ManagersPath(), "%v; its package and environment managers are ignored until it is fixed", err)
}

// checkRetiredConfigFile warns about a config.json left in the config
// directory. git-hop does not read it (settings live in git config hop.*),
// so it only misleads whoever edits it; a warning, since nothing breaks.
// doctor never deletes it, --fix included: its content is the user's.
func checkRetiredConfigFile(r *doctorReport) {
	path := config.RetiredConfigPath()
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return
	}
	output.Warn("%s is not read; git-hop settings live in git config hop.*", path)
	output.Hint("you can delete it by hand; keep a setting it held with\n'git config --global hop.<key> <value>'")
	r.record(doctorKindWarning, doctorCheckConfig, path, "not read; git-hop settings live in git config hop.*; you can delete the file")
}

// checkMigrationDebris reports and, under --fix, unsets the migration
// leftovers.
func checkMigrationDebris(l *config.GlobalLoader, opts doctorOpts, r *doctorReport) {
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

// checkStaleRetired warns about keys of retired settings in one git config
// scope and, under --fix, unsets them there. Nothing reads them, so they
// are warnings and leave the exit status alone. It returns how many keys
// it found.
func checkStaleRetired(scope config.ConfigScope, opts doctorOpts, r *doctorReport) int {
	where := "git config " + scope.String()
	stale, err := scope.StaleRetiredSettings()
	if err != nil {
		output.Warn("Could not check %s for retired settings: %v", where, err)
		r.record(doctorKindWarning, doctorCheckConfig, where, "could not check for retired settings: %v", err)
		return 0
	}

	for _, s := range stale {
		msg := fmt.Sprintf("retired setting %s in %s is no longer used", s.Key, where)
		if s.Replacement != "" {
			msg = fmt.Sprintf("retired setting %s in %s is ignored; the setting is now %s", s.Key, where, s.Replacement)
		}
		output.Warn("%s", msg)
		r.record(doctorKindWarning, doctorCheckConfig, s.Key, "%s; run 'git hop doctor --fix' to unset it", msg)
		if !opts.fix {
			if s.Replacement != "" {
				output.Hint("to turn %s on, run 'git config %s true'", s.Replacement, setArgs(scope, s.Replacement))
			}
			continue
		}
		if !opts.mutating() {
			output.Info("[dry-run] Would unset %s from %s", s.Key, where)
			r.repaired(opts, doctorCheckConfig, s.Key, "unset from %s", where)
			continue
		}
		if err := scope.RemoveStaleRetiredSetting(s.Key); err != nil {
			output.Error("Failed to unset %s from %s: %v", s.Key, where, err)
			r.failed(doctorCheckConfig, s.Key, "unset from %s: %v", where, err)
			continue
		}
		output.Info("Unset %s from %s", s.Key, where)
		r.repaired(opts, doctorCheckConfig, s.Key, "unset from %s", where)
	}
	return len(stale)
}

// setArgs is how to name key to `git config` for the scope: --global
// explicitly, the hub's own config by running it inside the hub.
func setArgs(scope config.ConfigScope, key string) string {
	if scope.String() == "--global" {
		return "--global " + key
	}
	return key
}
