package cmd

import (
	"strings"

	"github.com/spf13/afero"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/detector"
	"hop.top/git/internal/git"
	"hop.top/git/internal/output"
)

// Every command that honors the global --dry-run is listed here; any other
// command refuses the flag with a usage error (see cli.SupportDryRun).
//
// init, repair and env gc are absent because they register their own local
// --dry-run, which shadows the global flag. env start/stop/generate,
// __notify-chdir and upgrade are absent on purpose: they drive external
// managers or hooks whose effects cannot be previewed faithfully.
func init() {
	cli.SupportDryRun(
		// Preview their writes and stop before the first one.
		addCmd, removeCmd, moveCmd, mergeCmd, pruneCmd, doctorCmd,
		// Write nothing.
		listCmd, statusCmd, completionCmd, currentPathCmd,
	)
}

// refuseDryRun ends a preview whose real run would fail at the same point,
// with the same exit status the real run would have.
func refuseDryRun(action string, err error) {
	output.Fatal("[dry-run] would refuse to %s: %v", action, err)
}

// describeSafety renders a safety probe as the gate reads it.
func describeSafety(s branchSafety) string {
	parts := []string{"not merged", "not pushed", "not clean"}
	if s.Merged {
		parts[0] = "merged"
	}
	if s.Pushed {
		parts[1] = "pushed"
	}
	if s.Clean {
		parts[2] = "clean"
	}
	return strings.Join(parts, ", ")
}

// previewBranchDeletion reports the local and, when asked, remote deletion
// of branch. The remote is never probed: previewing must not wait on the
// network any more than a local-only removal does.
func previewBranchDeletion(branch string, local, remote bool) {
	if local {
		output.Info("[dry-run] Would delete local branch '%s'", branch)
	}
	if remote {
		output.Info("[dry-run] Would delete branch '%s' on origin, if it exists", branch)
	}
}

// previewDetector reports the git-flow action a real run would take for
// branch; action is the git-flow verb (e.g. "finish" on remove). Detection
// only reads git config; the generic detector's actions are no-ops, so
// git-flow is the one detector with an effect worth naming.
func previewDetector(fs afero.Fs, g git.GitInterface, branch, hubPath, action string) error {
	gitflow := detector.NewGitFlowNextDetector(g)
	mgr := detector.NewManager(fs, g)
	mgr.Register(gitflow)
	mgr.Register(detector.NewGenericDetector(detector.DefaultGenericConfig()))
	info, err := mgr.DetectBranch(branch, hubPath)
	if err != nil {
		return err
	}
	if info != nil && info.Source == gitflow.Name() {
		output.Info("[dry-run] Would run 'git flow %s %s %s'", info.Type, action, info.Name)
	}
	return nil
}
