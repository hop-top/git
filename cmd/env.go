package cmd

import (
	"os"
	"path/filepath"

	"github.com/jadb/git-hop/internal/cli"
	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/git"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/jadb/git-hop/internal/output"
	"github.com/jadb/git-hop/internal/services"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage the environment lifecycle",
}

var envStartCmd = &cobra.Command{
	Use:     "start",
	Aliases: []string{"up"},
	Short:   "Start the environment services",
	Run: func(cmd *cobra.Command, args []string) {
		runEnvCommand("start")
	},
}

var envStopCmd = &cobra.Command{
	Use:     "stop",
	Aliases: []string{"down"},
	Short:   "Stop the environment services",
	Run: func(cmd *cobra.Command, args []string) {
		runEnvCommand("stop")
	},
}

func runEnvCommand(action string) {
	fs := afero.NewOsFs()
	g := git.New()

	cwd, err := os.Getwd()
	if err != nil {
		output.Fatal("Failed to get current directory: %v", err)
	}

	if !g.IsInsideWorkTree(cwd) {
		output.Fatal("Not inside a git worktree. Please run this command from a worktree.")
	}

	root, err := g.GetRoot(cwd)
	if err != nil {
		output.Fatal("Failed to get git root: %v", err)
	}

	// Load global config
	globalLoader := config.NewGlobalLoader()
	globalConfig, err := globalLoader.Load()
	if err != nil {
		output.Warn("Failed to load global config, using defaults: %v", err)
		globalConfig = globalLoader.GetDefaults()
	}

	// Load environment managers
	managers, err := services.LoadEnvManagers(globalConfig)
	if err != nil {
		output.Fatal("Failed to load environment managers: %v", err)
	}

	// Try to find hub and get repo config
	var hubConfig *config.HubConfig
	var repoPath string
	var branch string
	hubPath, err := hop.FindHub(fs, cwd)
	if err == nil {
		hub, err := hop.LoadHub(fs, hubPath)
		if err == nil {
			hubConfig = hub.Config
			// Get hopspace path
			dataHome := hop.GetGitHopDataHome()
			repoPath = hop.GetHopspacePath(dataHome, hub.Config.Repo.Org, hub.Config.Repo.Repo)
			// Get branch name from worktree path
			branch = filepath.Base(root)
		}
	}

	// Detect which environment manager to use
	manager, err := services.DetectEnvManager(root, hubConfig, managers)
	if err != nil {
		output.Fatal("Failed to detect environment manager: %v", err)
	}

	if manager == nil {
		output.Info("No environment manager detected, skipping")
		return
	}

	output.Info("Environment Manager: %s", manager.Name)

	switch action {
	case "start":
		// Setup dependencies before starting services
		if repoPath != "" && branch != "" {
			output.Info("Ensuring dependencies...")
			depsManager, err := services.NewDepsManager(fs, repoPath, globalConfig)
			if err != nil {
				output.Warn("Failed to initialize dependency manager: %v", err)
			} else {
				if err := depsManager.EnsureDeps(root, branch); err != nil {
					output.Warn("Failed to ensure dependencies: %v", err)
				} else {
					output.Info("Dependencies ready.")
				}
			}
		}

		if err := manager.Start(root, branch, repoPath, hubConfig); err != nil {
			output.Fatal("Failed to start environment: %v", err)
		}
	case "stop":
		if err := manager.Stop(root, branch, repoPath, hubConfig); err != nil {
			output.Fatal("Failed to stop environment: %v", err)
		}
	}
}

func init() {
	envCmd.AddCommand(envStartCmd)
	envCmd.AddCommand(envStopCmd)
	cli.RootCmd.AddCommand(envCmd)
}
