package cmd

import (
	"os"
	"path/filepath"

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
	d := docker.New()

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

	switch action {
	case "start":
		// Load global config
		globalLoader := config.NewGlobalLoader()
		globalConfig, err := globalLoader.Load()
		if err != nil {
			output.Warn("Failed to load global config, using defaults: %v", err)
			globalConfig = globalLoader.GetDefaults()
		}

		// Try to find hub and get repo path
		hubPath, err := hop.FindHub(fs, cwd)
		if err == nil {
			hub, err := hop.LoadHub(fs, hubPath)
			if err == nil {
				// Get hopspace path
				dataHome := hop.GetGitHopDataHome()
				hopspacePath := hop.GetHopspacePath(dataHome, hub.Config.Repo.Org, hub.Config.Repo.Repo)

				// Get branch name from worktree path
				branch := filepath.Base(root)

				// Setup dependencies before starting services
				output.Info("Ensuring dependencies...")
				depsManager, err := services.NewDepsManager(fs, hopspacePath, globalConfig)
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
		}

		// Check if docker environment exists before trying to start services
		if d.HasDockerEnv(root) {
			output.Info("Starting services...")
			if err := d.ComposeUp(root, true); err != nil {
				output.Fatal("Failed to start services: %v", err)
			}
			output.Info("Services started.")
		} else {
			output.Info("No docker environment found. Dependencies are ready but no services to start.")
		}
	case "stop":
		// Check if docker environment exists before trying to stop services
		if d.HasDockerEnv(root) {
			output.Info("Stopping services...")
			if err := d.ComposeStop(root); err != nil {
				output.Fatal("Failed to stop services: %v", err)
			}
			output.Info("Services stopped.")
		} else {
			output.Info("No docker environment found. Nothing to stop.")
		}
	}
}

func init() {
	envCmd.AddCommand(envStartCmd)
	envCmd.AddCommand(envStopCmd)
	cli.RootCmd.AddCommand(envCmd)
}
