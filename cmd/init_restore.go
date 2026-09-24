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
// directory. An occupied location is replaced only with --force.
func handleRestore(fs afero.Fs, g git.GitInterface, backupPath string, force bool) {
	abs, err := filepath.Abs(backupPath)
	if err != nil {
		output.Error("Failed to resolve backup path %s: %v", backupPath, err)
		os.Exit(1)
	}

	output.Info("Restoring from backup...")

	target, err := hop.NewConverter(fs, g).RestoreToOriginal(abs, force)
	if errors.Is(err, hop.ErrRestoreTargetNotEmpty) {
		output.Error("Restore refused: %v", err)
		output.Hint("A backup restores to the location it was taken from. To replace\n"+
			"what is there now (it is deleted) with the backup:\n"+
			"  git hop init --restore %s --force", abs)
		os.Exit(1)
	}
	if err != nil {
		output.Error("Restore failed: %v", err)
		os.Exit(1)
	}

	fmt.Println("\nRestore successful!")
	fmt.Printf("Repository restored to: %s\n", target)
	fmt.Printf("Original backup: %s\n", abs)
	output.Hint("You can now inspect or delete the backup:\n  rm -rf %s", abs)
}

// restoreHint tells the user how to undo a conversion from its kept
// backup. After a conversion the original location holds the new hub,
// so the command needs --force to replace it.
func restoreHint(backupPath string) string {
	return "To restore the repository from this backup, replacing what is at\n" +
		"its original location:\n" +
		"  git hop init --restore " + backupPath + " --force"
}
