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
	var org, repo string
	hubPath, err := hop.FindHub(fs, cwd)
	if err == nil {
		hub, err := hop.LoadHub(fs, hubPath)
		if err == nil {
			hubConfig = hub.Config
			org = hub.Config.Repo.Org
			repo = hub.Config.Repo.Repo
			// Get hopspace path
			dataHome := hop.GetGitHopDataHome()
			repoPath = hop.GetHopspacePath(dataHome, org, repo)
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

	// Compute override path from cache (if it exists)
	var overridePath string
	if org != "" && repo != "" && branch != "" {
		candidate := hop.GetComposeOverrideCachePath(org, repo, branch)
		if _, err := os.Stat(candidate); err == nil {
			overridePath = candidate
		}
	}

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

		if err := manager.Start(root, branch, repoPath, hubConfig, overridePath); err != nil {
			output.Fatal("Failed to start environment: %v", err)
		}
	case "stop":
		if err := manager.Stop(root, branch, repoPath, hubConfig, overridePath); err != nil {
			output.Fatal("Failed to stop environment: %v", err)
		}
	}
}

var envGenerateCmd = &cobra.Command{
	Use:   "generate",
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

		branch := filepath.Base(root)
		org := hub.Config.Repo.Org
		repo := hub.Config.Repo.Repo

		// Load hopspace for ports/volumes config
		var hopspacePath string
		_, err = hop.LoadHopspace(fs, hubPath)
		if err != nil {
			dataHome := hop.GetGitHopDataHome()
			hopspacePath = hop.GetHopspacePath(dataHome, org, repo)
			_, err = hop.LoadHopspace(fs, hopspacePath)
			if err != nil {
				output.Fatal("Failed to load hopspace")
			}
		} else {
			hopspacePath = hubPath
		}

		// Load ports and volumes config
		portsLoader := config.NewLoader(fs)
		portsCfg, err := portsLoader.LoadPortsConfig(hopspacePath)
		if err != nil {
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

		if !d.HasDockerEnv(root) {
			output.Info("No Docker environment detected, skipping")
			return
		}

		envMgr := services.NewEnvManager(fs, portsCfg, volsCfg, d)
		branchPorts, branchVols, overridePath, err := envMgr.Generate(branch, root, org, repo)
		if err != nil {
			output.Fatal("Failed to generate environment: %v", err)
		}

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

		output.Info("Environment generated for '%s'", branch)
		if overridePath != "" {
			output.Info("Override: %s", overridePath)
		}
	},
}

func init() {
	envCmd.AddCommand(envStartCmd)
	envCmd.AddCommand(envStopCmd)
	envCmd.AddCommand(envGenerateCmd)
	cli.RootCmd.AddCommand(envCmd)
}
