package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jadb/git-hop/internal/cli"
	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/jadb/git-hop/internal/output"
	"github.com/jadb/git-hop/internal/tui"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List hubs, hopspaces, and branches",
	Run: func(cmd *cobra.Command, args []string) {
		fs := afero.NewOsFs()
		cwd, err := os.Getwd()
		if err != nil {
			output.Fatal("Failed to get current directory: %v", err)
		}

		if hop.IsHub(fs, cwd) {
			hub, err := hop.LoadHub(fs, cwd)
			if err != nil {
				output.Fatal("Failed to load hub: %v", err)
			}

			output.Info("Hub: %s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)

			t := tui.NewTable([]interface{}{"Branch", "Path", "Status", "Ports", "Services"})

			// Load ports config
			dataHome := os.Getenv("GIT_HOP_DATA_HOME")
			if dataHome == "" {
				home, _ := os.UserHomeDir()
				dataHome = filepath.Join(home, ".local", "share", "git-hop")
			}
			hopspacePath := hop.GetHopspacePath(dataHome, hub.Config.Repo.Org, hub.Config.Repo.Repo)

			portsLoader := config.NewLoader(fs)
			portsCfg, _ := portsLoader.LoadPortsConfig(hopspacePath)

			for name, b := range hub.Config.Branches {
				state := "missing"
				if _, err := fs.Stat(filepath.Join(hub.Path, b.Path)); err == nil {
					state = "active"
				}

				portsStr := ""
				servicesStr := ""
				if portsCfg != nil {
					if bp, ok := portsCfg.Branches[b.HopspaceBranch]; ok && len(bp.Ports) > 0 {
						var minPort, maxPort int
						var servicesList []string
						first := true
						for svc, p := range bp.Ports {
							if first || p < minPort {
								minPort = p
							}
							if first || p > maxPort {
								maxPort = p
							}
							first = false
							servicesList = append(servicesList, svc)
						}
						sort.Strings(servicesList)
						portsStr = fmt.Sprintf("%d-%d", minPort, maxPort)
						servicesStr = strings.Join(servicesList, ", ")
					}
				}

				t.AddRow(name, b.Path, state, portsStr, servicesStr)
			}

			t.Render()
			return
		}

		// TODO: List all hubs/hopspaces if not in a hub
		output.Info("Not in a hub. Listing all hopspaces...")

		dataHome := os.Getenv("GIT_HOP_DATA_HOME")
		if dataHome == "" {
			home, _ := os.UserHomeDir()
			dataHome = filepath.Join(home, ".local", "share", "git-hop")
		}

		// Walk dataHome to find hopspaces
		// This is a bit expensive, but fine for list.
		// Structure: dataHome/org/repo/hop.json

		// For now just print message
		output.Info("Scanning %s", dataHome)
	},
}

func init() {
	cli.RootCmd.AddCommand(listCmd)
}
