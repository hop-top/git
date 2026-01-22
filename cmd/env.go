package cmd

import (
	"os"

	"github.com/jadb/git-hop/internal/cli"
	"github.com/jadb/git-hop/internal/docker"
	"github.com/jadb/git-hop/internal/git"
	"github.com/jadb/git-hop/internal/output"
	"github.com/spf13/cobra"
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage the environment lifecycle",
}

var envStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the environment services",
	Run: func(cmd *cobra.Command, args []string) {
		runEnvCommand("start")
	},
}

var envStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the environment services",
	Run: func(cmd *cobra.Command, args []string) {
		runEnvCommand("stop")
	},
}

func runEnvCommand(action string) {
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
		output.Info("Starting services...")
		if err := d.ComposeUp(root, true); err != nil {
			output.Fatal("Failed to start services: %v", err)
		}
		output.Info("Services started.")
	case "stop":
		output.Info("Stopping services...")
		if err := d.ComposeStop(root); err != nil {
			output.Fatal("Failed to stop services: %v", err)
		}
		output.Info("Services stopped.")
	}
}

func init() {
	envCmd.AddCommand(envStartCmd)
	envCmd.AddCommand(envStopCmd)
	cli.RootCmd.AddCommand(envCmd)
}
