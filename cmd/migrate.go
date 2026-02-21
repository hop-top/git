package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/state"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate old git-hop data to new XDG-based structure",
	Long: `Migrate existing git-hop data to the new XDG-compliant directory structure.

This command:
  1. Reads the old hops.json registry
  2. Converts it to the new state.json format
  3. Backs up old data
  4. Writes the new state file

The new structure separates configuration from state:
  - Config: $XDG_CONFIG_HOME/git-hop/config.json
  - State:  $XDG_STATE_HOME/git-hop/state.json

Old data is backed up to: $HOME/.git-hop-backup/<timestamp>/
`,
	Run: runMigrate,
}

func init() {
	cli.RootCmd.AddCommand(migrateCmd)
}

func runMigrate(cmd *cobra.Command, args []string) {
	fs := afero.NewOsFs()

	output.Info("Starting migration to XDG-based structure...")

	// Load old registry
	output.Info("\nLoading old registry...")
	oldRegistry := hop.LoadRegistry(fs)

	if oldRegistry.Config == nil || len(oldRegistry.Config.Hops) == 0 {
		output.Info("No old data found to migrate.")
		output.Info("If you have existing repositories, run 'git hop init' in each one.")
		return
	}

	output.Info("Found %d hop(s) in old registry", len(oldRegistry.Config.Hops))

	// Create new state
	output.Info("\nInitializing new state structure...")
	newState := state.NewState()

	// Migrate registry
	output.Info("\nMigrating repositories and worktrees...")
	if err := hop.MigrateRegistry(fs, oldRegistry, newState); err != nil {
		output.Fatal("Migration failed: %v", err)
	}

	output.Success("Migrated %d repositor(ies)", len(newState.Repositories))
	for repoID, repo := range newState.Repositories {
		output.Info("  - %s (%d worktree(s))", repoID, len(repo.Worktrees))
	}

	// Backup old data
	output.Info("\nBacking up old data...")
	backupDir, err := backupOldData(fs)
	if err != nil {
		output.Warn("Failed to backup old data: %v", err)
	} else {
		output.Success("Old data backed up to: %s", backupDir)
	}

	// Save new state
	output.Info("\nWriting new state file...")
	if err := state.SaveState(fs, newState); err != nil {
		output.Fatal("Failed to save new state: %v", err)
	}

	statePath := filepath.Join(state.GetStateHome(), "state.json")
	output.Success("New state saved to: %s", statePath)

	// Rename old files
	output.Info("\nRenaming old files...")
	if err := renameOldFiles(fs); err != nil {
		output.Warn("Failed to rename old files: %v", err)
	} else {
		output.Success("Old files renamed with .old extension")
	}

	output.Info("\n✓ Migration complete!")
	output.Info("\nNext steps:")
	output.Info("  1. Run: git hop doctor --fix")
	output.Info("  2. Test: git hop list")
	output.Info("  3. Verify your worktrees are accessible")
	output.Info("\nIf you encounter issues, restore from backup:")
	output.Info("  cp %s/hops.json %s", backupDir, hop.GetHopsRegistryPath())
}

func backupOldData(fs afero.Fs) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	timestamp := time.Now().Format("20060102-150405")
	backupDir := filepath.Join(homeDir, ".git-hop-backup", timestamp)

	if err := fs.MkdirAll(backupDir, 0755); err != nil {
		return "", err
	}

	// Backup hops.json (registry)
	oldRegistryPath := hop.GetHopsRegistryPath()
	if exists, _ := afero.Exists(fs, oldRegistryPath); exists {
		content, err := afero.ReadFile(fs, oldRegistryPath)
		if err == nil {
			backupPath := filepath.Join(backupDir, "hops.json")
			if err := afero.WriteFile(fs, backupPath, content, 0644); err != nil {
				return backupDir, fmt.Errorf("failed to backup hops.json: %w", err)
			}
		}
	}

	// Backup global.json if it exists
	oldGlobalPath := hop.GetGlobalConfigPath()
	if exists, _ := afero.Exists(fs, oldGlobalPath); exists {
		content, err := afero.ReadFile(fs, oldGlobalPath)
		if err == nil {
			backupPath := filepath.Join(backupDir, "global.json")
			afero.WriteFile(fs, backupPath, content, 0644)
		}
	}

	return backupDir, nil
}

func renameOldFiles(fs afero.Fs) error {
	oldRegistryPath := hop.GetHopsRegistryPath()
	if exists, _ := afero.Exists(fs, oldRegistryPath); exists {
		newPath := oldRegistryPath + ".old"
		if err := fs.Rename(oldRegistryPath, newPath); err != nil {
			return fmt.Errorf("failed to rename hops.json: %w", err)
		}
	}

	return nil
}
