package cmd

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jadb/git-hop/internal/cli"
	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/docker"
	"github.com/jadb/git-hop/internal/git"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/jadb/git-hop/internal/output"
	"github.com/jadb/git-hop/internal/services"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:   "add [branch]",
	Short: "Add a new worktree and environment",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		branch := args[0]
		fs := afero.NewOsFs()
		g := git.New()
		d := docker.New()

		cwd, err := os.Getwd()
		if err != nil {
			output.Fatal("Failed to get current directory: %v", err)
		}

		// Find the hub by searching up the directory tree
		hubPath, err := hop.FindHub(fs, cwd)
		if err != nil {
			output.Fatal("Not in a git-hop hub. Please run 'git hop <uri>' to clone first, or initialize a hub.")
		}

		hub, err := hop.LoadHub(fs, hubPath)
		if err != nil {
			output.Fatal("Failed to load hub: %v", err)
		}

		// Load Hopspace - try local first, then global
		var hopspace *hop.Hopspace
		var hopspacePath string

		// Try local hopspace first (in hub directory)
		localHopspacePath := hubPath
		hopspace, err = hop.LoadHopspace(fs, localHopspacePath)
		if err != nil {
			// Try global hopspace (in data directory)
			dataHome := hop.GetGitHopDataHome()
			globalHopspacePath := hop.GetHopspacePath(dataHome, hub.Config.Repo.Org, hub.Config.Repo.Repo)
			hopspace, err = hop.LoadHopspace(fs, globalHopspacePath)
			if err != nil {
				output.Fatal("Failed to load hopspace locally at %s or globally at %s", localHopspacePath, globalHopspacePath)
			}
			hopspacePath = globalHopspacePath
		} else {
			hopspacePath = localHopspacePath
		}

		output.Info("Adding branch %s (v2)...", branch)

		// Create Worktree in the current hub
		wm := hop.NewWorktreeManager(fs, g)
		worktreePath, err := wm.CreateWorktree(hopspace, hubPath, branch)
		if err != nil {
			output.Fatal("Failed to create worktree: %v", err)
		}

		// Register in Hopspace
		if err := hopspace.RegisterBranch(branch, worktreePath); err != nil {
			output.Fatal("Failed to register branch in hopspace: %v", err)
		}

		// Add to Hub
		if err := hub.AddBranch(branch, branch, worktreePath); err != nil {
			output.Fatal("Failed to add branch to hub: %v", err)
		}

		// Generate Environment
		// We need to load ports and volumes config
		portsLoader := config.NewLoader(fs)
		portsCfg, err := portsLoader.LoadPortsConfig(hopspacePath)
		if err != nil {
			// If missing, maybe init default?
			// For now, assume it exists or we create empty.
			// Let's create a default if missing.
			portsCfg = &config.PortsConfig{
				AllocationMode: "incremental",
				BaseRange:      config.PortRange{Start: 10000, End: 20000},
				Branches:       make(map[string]config.BranchPorts),
			}
		}

		volsLoader := config.NewLoader(fs)
		volsCfg, err := volsLoader.LoadVolumesConfig(hopspacePath)
		if err != nil {
			volsCfg = &config.VolumesConfig{
				BasePath: filepath.Join(hopspacePath, "volumes"),
				Branches: make(map[string]config.BranchVolumes),
			}
		}

		envMgr := services.NewEnvManager(fs, portsCfg, volsCfg, d)
		branchPorts, branchVols, err := envMgr.Generate(branch, worktreePath)
		if err != nil {
			output.Error("Failed to generate environment: %v", err)
			// Don't fatal, just warn? Or fatal?
			// Specs say "Implement service/env initialization triggers".
		} else {
			// Update configs
			portsCfg.Branches[branch] = *branchPorts
			volsCfg.Branches[branch] = *branchVols

			writer := config.NewWriter(fs)
			if err := writer.WritePortsConfig(hopspacePath, portsCfg); err != nil {
				output.Error("Failed to save ports config: %v", err)
			}
			if err := writer.WriteVolumesConfig(hopspacePath, volsCfg); err != nil {
				output.Error("Failed to save volumes config: %v", err)
			}
		}

		output.Info("Created hopspace for '%s'", branch)

		relPath, _ := filepath.Rel(cwd, worktreePath)
		if !strings.HasPrefix(relPath, ".") && !filepath.IsAbs(relPath) {
			relPath = "./" + relPath
		}
		output.Info("Worktree: %s", relPath)

		if branchPorts != nil && len(branchPorts.Ports) > 0 {
			var minPort, maxPort int
			var servicesList []string
			first := true
			for svc, p := range branchPorts.Ports {
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

			output.Info("Ports: %d-%d", minPort, maxPort)
			output.Info("Services: %s", strings.Join(servicesList, ", "))
		}
	},
}

func init() {
	cli.RootCmd.AddCommand(addCmd)
}
