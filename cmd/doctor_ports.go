package cmd

import (
	"github.com/spf13/afero"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
)

// checkPorts reports every port two worktrees hold, across every hub
// git-hop knows of. Read-only, --fix included: re-porting a worktree
// breaks an environment that is running, so it is left to
// 'git hop env generate' in the worktree reported, the one whose hub was
// set up later.
func checkPorts(fs afero.Fs, hubPath string, r *doctorReport) {
	output.Info("\n=== Checking Ports ===")
	recs, err := services.LoadEnvRecords(fs, hubPath)
	if err != nil {
		output.Warn("Cannot read the port allocations of every hub: %v", err)
		r.record(doctorKindWarning, doctorCheckPorts, "", "cannot read the port allocations of every hub: %v", err)
	}
	collisions := recs.Collisions()
	if len(collisions) == 0 {
		output.Info("No port is allocated twice")
		return
	}
	for _, c := range collisions {
		output.Warn("port %d (%s) of %s is also allocated to %s of %s; run 'git hop env generate' there to allocate new ports",
			c.Port, c.Service, c.Claim.Worktree(), c.Other.Branch, c.Other.Hub)
		r.record(doctorKindWarning, doctorCheckPorts, c.Claim.Worktree(),
			"port %d (%s) is also allocated to %s of %s; run 'git hop env generate' here to allocate new ports",
			c.Port, c.Service, c.Other.Branch, c.Other.Hub)
	}
}
