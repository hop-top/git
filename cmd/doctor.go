package cmd

import (
	"os"
	"path/filepath"

	"github.com/jadb/git-hop/internal/cli"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/jadb/git-hop/internal/output"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check and repair the environment",
	Run: func(cmd *cobra.Command, args []string) {
		fs := afero.NewOsFs()
		cwd, err := os.Getwd()
		if err != nil {
			output.Fatal("Failed to get current directory: %v", err)
		}

		output.Info("Running diagnostics...")
		issuesFound := false

		// Check Hub
		if hop.IsHub(fs, cwd) {
			output.Info("Checking Hub at %s...", cwd)
			hub, err := hop.LoadHub(fs, cwd)
			if err != nil {
				output.Error("Failed to load hub config: %v", err)
				issuesFound = true
			} else {
				for name, b := range hub.Config.Branches {
					// Check symlink
					linkPath := filepath.Join(hub.Path, b.Path)
					if _, err := fs.Stat(linkPath); err != nil {
						output.Error("Broken link for branch %s: %v", name, err)
						issuesFound = true
					}
				}
			}
		} else {
			output.Info("Not in a hub. Skipping hub checks.")
		}

		// Check Hopspace
		// We can only check hopspace if we know where it is.
		// If in a hub, we can derive it.
		// If not, we can scan?
		// For now, skip if not in hub or explicit path not provided.

		if !issuesFound {
			output.Info("No issues found.")
		} else {
			output.Info("Issues found. Please fix them manually or run with --fix (not implemented yet).")
		}
	},
}

func init() {
	cli.RootCmd.AddCommand(doctorCmd)
}
