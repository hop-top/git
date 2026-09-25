package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"

	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
)

// handleRestore runs `init --restore <backup>`: the backup goes back to
// the location its metadata says it was taken from, never to the cwd, so
// it works wherever hop.backup.path put the backup and from any
// directory. An occupied location is refused; --force moves it aside
// (never deletes it) and restores in its place.
//
// A dry run makes the same checks and refuses the same way, with the
// same exit status, then reports what a real run would do instead of
// doing it.
func handleRestore(fs afero.Fs, g git.GitInterface, backupPath string, force, dryRun bool) {
	abs, err := filepath.Abs(backupPath)
	if err != nil {
		output.Error("Failed to resolve backup path %s: %v", backupPath, err)
		os.Exit(1)
	}

	if !dryRun {
		output.Info("Restoring from backup...")
	}

	converter := hop.NewConverter(fs, g)
	converter.DryRun = dryRun
	res, err := converter.RestoreToOriginal(abs, hop.RestoreOptions{Replace: force})
	if errors.Is(err, hop.ErrRestoreTargetNotEmpty) {
		output.Error("Restore refused: %v", err)
		output.Hint("A backup restores to the location it was taken from. To move what\n"+
			"is there now aside (to %s.pre-restore-<time>, nothing is deleted)\n"+
			"and restore the backup in its place:\n"+
			"  %s", res.Target, initRestoreCommand(abs, true))
		os.Exit(1)
	}
	if err != nil {
		output.Error("Restore failed: %v", err)
		os.Exit(1)
	}

	if output.IsStructured() {
		emitResult(initRunCmd, initResult{
			Action:     initActionRestored,
			Hub:        res.Target,
			Backup:     abs,
			DryRun:     dryRun,
			MovedAside: res.MovedAside,
			Worktrees:  []initWorktree{},
		})
		return
	}
	if dryRun {
		previewRestore(fs, abs, res)
		return
	}

	fmt.Println("\nRestore successful!")
	fmt.Printf("Repository restored to: %s\n", res.Target)
	if res.MovedAside != "" {
		fmt.Printf("Previous contents moved aside to: %s\n", res.MovedAside)
		output.Hint("Once satisfied with the restore, delete the previous contents:\n"+
			"  rm -rf %s\n"+
			"To go back to them instead:\n"+
			"  mv %s %s.restored && mv %s %s",
			res.MovedAside, res.Target, res.Target, res.MovedAside, res.Target)
	}
	fmt.Printf("Original backup: %s\n", abs)
	output.Hint("You can now inspect or delete the backup:\n  rm -rf %s", abs)
}

// previewRestore is the dry-run report of a restore that passed every
// check: where the backup goes, what is there now, and what would move.
func previewRestore(fs afero.Fs, backupPath string, res hop.RestoreResult) {
	fmt.Println(initDryRunBanner)
	fmt.Printf("Backup: %s\n", backupPath)
	fmt.Printf("Target: %s (recorded in backup-info.json)\n", res.Target)
	switch exists, _ := afero.Exists(fs, res.Target); {
	case res.Occupied:
		fmt.Println("Target is occupied")
		fmt.Printf("Would move %s aside to %s\n", res.Target, res.MovedAside)
	case exists:
		fmt.Println("Target is an empty directory; nothing would be moved aside")
	default:
		fmt.Println("Target does not exist; nothing would be moved aside")
	}
	fmt.Printf("Would restore %s to %s\n", filepath.Join(backupPath, "original"), res.Target)
	output.Hint("To restore, run:\n  %s", initRestoreCommand(backupPath, res.Occupied))
}

// restoreHint tells the user how to undo a conversion from its kept
// backup. After a conversion the original location holds the new hub,
// so the command needs --force, which moves the hub aside.
func restoreHint(backupPath string) string {
	return "To restore the repository from this backup (the hub now at its\n" +
		"original location is moved aside, not deleted):\n" +
		"  " + initRestoreCommand(backupPath, true)
}
