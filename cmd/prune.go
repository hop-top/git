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

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove orphaned data",
	Run: func(cmd *cobra.Command, args []string) {
		fs := afero.NewOsFs()
		cwd, err := os.Getwd()
		if err != nil {
			output.Fatal("Failed to get current directory: %v", err)
		}

		hubPath, err := hop.FindHub(fs, cwd)
		if err == nil {
			output.Info("Pruning Hub at %s...", hubPath)
			hub, err := hop.LoadHub(fs, hubPath)
			if err != nil {
				output.Fatal("Failed to load hub: %v", err)
			}

			for name, b := range hub.Config.Branches {
				linkPath := filepath.Join(hub.Path, b.Path)
				if _, err := fs.Stat(linkPath); err != nil {
					if os.IsNotExist(err) {
						output.Info("Removing orphaned branch entry: %s", name)
						if err := hub.RemoveBranch(name); err != nil {
							output.Error("Failed to remove branch entry: %v", err)
						}
					}
				}
			}
			output.Info("Hub prune complete.")
		} else {
			output.Info("Not in a hub. Skipping hub prune.")
		}

		// TODO: Prune hopspaces, ports, volumes
	},
}

func init() {
	cli.RootCmd.AddCommand(pruneCmd)
}
