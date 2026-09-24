package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/afero"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
)

// checkHubRegistration reports the current hub when state does not record
// it, or records it without some of its worktrees, and under --fix
// records the rest (hop.RegisterNewHub). Hubs converted before
// conversions recorded themselves have a hop.json and no state entry, so
// list, status --all and prune --all never see them.
//
// Only worktrees whose directory is there are recorded (hubFromConfig):
// the hub check, which runs first, has recreated what it could. The
// registration merges with state and never overwrites it: the
// repository's other hubs and their worktrees, of the same branches or
// not, are kept.
//
// Outside a hub, or with a state file that cannot be read (the state
// check warns about that), there is nothing to check against.
func checkHubRegistration(fs afero.Fs, hubPath string, opts doctorOpts, r *doctorReport) {
	if hubPath == "" {
		return
	}
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		return
	}
	st, err := state.LoadState(fs)
	if err != nil {
		return
	}

	h := hubFromConfig(fs, hub)
	plan := hop.PlanHubRegistration(st, h)
	if plan.Empty() {
		return
	}

	missing := unregisteredDescription(plan)
	repair := fmt.Sprintf("register hub and %d worktree(s) in state", len(plan.Worktrees))
	if !plan.Hub {
		repair = fmt.Sprintf("register %d worktree(s) in state", len(plan.Worktrees))
	}
	r.issue(doctorCheckState, hubPath, "%s", missing)

	switch {
	case !opts.fix:
		output.Error("%s", missing)
		output.Info("  Run 'git hop doctor --fix' to register it")
	case !opts.mutating():
		output.Info("[dry-run] Would %s: %s", repair, hubPath)
		r.repaired(opts, doctorCheckState, hubPath, "%s", repair)
	default:
		if _, err := hop.RegisterNewHub(fs, h); err != nil {
			r.failed(doctorCheckState, hubPath, "%s: %v", repair, err)
			return
		}
		output.Info("Registered %s in state", hubPath)
		r.repaired(opts, doctorCheckState, hubPath, "%s", repair)
	}
}

// unregisteredDescription says what state lacks of the hub.
func unregisteredDescription(plan hop.HubRegistration) string {
	if plan.Hub {
		return "hub not registered in state; list, status --all and prune --all do not see it"
	}
	return fmt.Sprintf("hub registered in state without worktree(s): %s", strings.Join(plan.Branches(), ", "))
}
