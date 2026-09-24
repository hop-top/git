package cmd

import (
	"context"
	"errors"
	"os"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"hop.top/git/internal/cli"
	"hop.top/git/internal/config"
	"hop.top/git/internal/docker"
	"hop.top/git/internal/events"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
	"hop.top/kit/go/runtime/bus"
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage the environment lifecycle",
}

var envStartCmd = &cobra.Command{
	Use:     "start",
	Args:    cobra.NoArgs,
	Aliases: []string{"up"},
	Short:   "Start the environment services",
	Run: func(cmd *cobra.Command, args []string) {
		runEnvCommand("start")
	},
}

var envStopCmd = &cobra.Command{
	Use:     "stop",
	Args:    cobra.NoArgs,
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

	// Hub context is optional: outside a hub the worktree's own files
	// still select a manager.
	target := services.EnvTarget{Root: root}
	if hubPath, err := hop.FindHub(fs, cwd); err == nil {
		if hub, err := hop.LoadHub(fs, hubPath); err == nil {
			target.Hub = hub.Config
			target.HopspacePath = hop.ResolveHopspacePath(hubPath, hub.Config.Repo)
			target.Branch, _ = g.GetCurrentBranch(root)
		}
	}

	switch action {
	case "start":
		err := services.StartEnv(fs, target, globalConfig, cli.EventBus)
		if errors.Is(err, services.ErrNoEnvironment) {
			output.Info("No environment manager detected, skipping")
			return
		}
		if err != nil {
			output.Fatal("Failed to start environment: %v", err)
		}
	case "stop":
		manager, overridePath, err := services.ResolveEnv(target, globalConfig)
		if err != nil {
			output.Fatal("Failed to detect environment manager: %v", err)
		}
		if manager == nil {
			output.Info("No environment manager detected, skipping")
			return
		}
		output.Info("Environment Manager: %s", manager.Name)
		if err := manager.Stop(root, target.Branch, target.HopspacePath, target.Hub, overridePath); err != nil {
			output.Fatal("Failed to stop environment: %v", err)
		}
		_ = cli.EventBus.Publish(context.Background(), bus.NewEvent(
			events.EnvStopped, events.Source,
			events.EnvEvent{Action: "stop", Root: root, Branch: target.Branch},
		))
	}
}

var envGenerateCmd = &cobra.Command{
	Use:   "generate",
	Args:  cobra.NoArgs,
	Short: "Generate environment files (.env, override) for the current worktree",
	Run: func(cmd *cobra.Command, args []string) {
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

		// Find hub
		hubPath, err := hop.FindHub(fs, cwd)
		if err != nil {
			output.Fatal("Not in a git-hop hub. Please run from a hub worktree.")
		}

		hub, err := hop.LoadHub(fs, hubPath)
		if err != nil {
			output.Fatal("Failed to load hub: %v", err)
		}

		branch, _ := g.GetCurrentBranch(root)
		org := hub.Config.Repo.Org
		repo := hub.Config.Repo.Repo

		// Load hopspace for ports/volumes config
		hopspacePath := hop.ResolveHopspacePath(hubPath, hub.Config.Repo)
		if _, err := hop.LoadHopspace(fs, hopspacePath); err != nil {
			output.Fatal("Failed to load hopspace at %s: %v", hopspacePath, err)
		}

		env, err := services.GenerateWorktreeEnv(fs, d, hopspacePath, root, branch, org, repo)
		if err != nil {
			output.Fatal("Failed to generate environment: %v", err)
		}
		if env == nil {
			output.Info("No Docker environment detected, skipping")
			return
		}

		output.Info("Environment generated for '%s'", branch)
		if env.OverridePath != "" {
			output.Info("Override: %s", env.OverridePath)
		}
	},
}

func init() {
	envCmd.AddCommand(envStartCmd)
	envCmd.AddCommand(envStopCmd)
	envCmd.AddCommand(envGenerateCmd)
	cli.RootCmd.AddCommand(envCmd)
}
