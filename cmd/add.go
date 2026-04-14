package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/config"
	"hop.top/git/internal/detector"
	"hop.top/git/internal/docker"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
	"hop.top/git/internal/state"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"hop.top/kit/xdg"
)

var addCmd = &cobra.Command{
	Use:     "add [branch]",
	Aliases: []string{"create", "new"},
	Short:   "Add a new worktree and environment",
	Args:    cobra.ExactArgs(1),
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

		output.Info("Adding branch %s...", branch)

		// Load global config for worktree location
		globalLoader := config.NewGlobalLoader()
		globalConfig, err := globalLoader.Load()
		if err != nil {
			globalConfig = globalLoader.GetDefaults()
		}

		// Calculate worktree path (needed for pre-worktree-add hook)
		dataHome := hop.GetGitHopDataHome()
		ctx := hop.WorktreeLocationContext{
			HubPath:  hubPath,
			Branch:   branch,
			Org:      hub.Config.Repo.Org,
			Repo:     hub.Config.Repo.Repo,
			DataHome: dataHome,
		}
		worktreePath := hop.ExpandWorktreeLocation(globalConfig.Defaults.WorktreeLocation, ctx)
		worktreePath = filepath.Clean(worktreePath)

		repoID := fmt.Sprintf("github.com/%s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)

		// Create detector manager and register detectors
		detectorMgr := detector.NewManager(fs, g)
		detectorMgr.Register(detector.NewGitFlowNextDetector(g))
		detectorMgr.Register(detector.NewGenericDetector(detector.DefaultGenericConfig()))

		// Execute pre-add (detector OnAdd)
		detectorCtx := context.Background()
		branchInfo, err := detectorMgr.ExecutePreAdd(detectorCtx, branch, hubPath, worktreePath)
		if err != nil {
			output.Fatal("Branch type detector failed: %v", err)
		}

		// Execute pre-worktree-add hook with detector env vars
		hookRunner := hooks.NewRunner(fs)
		detectorEnv := detectorMgr.GetDetectorEnvVars(branchInfo)
		if err := hookRunner.ExecuteHookWithDetector("pre-worktree-add", worktreePath, repoID, branch, detectorEnv); err != nil {
			output.Fatal("Hook pre-worktree-add failed: %v", err)
		}

		// Create Worktree in the current hub
		wm := hop.NewWorktreeManager(fs, g)
		worktreePath, err = wm.CreateWorktreeTransactional(hopspace, hubPath, branch, globalConfig.Defaults.WorktreeLocation, hub.Config.Repo.Org, hub.Config.Repo.Repo)
		if err != nil {
			// Check if it's a state error
			if stateErr, ok := err.(*hop.StateError); ok {
				output.Error("Cannot create worktree due to state issues:")
				output.Error("  %s at %s: %s", stateErr.Type, stateErr.Path, stateErr.Message)
				output.Info("\nRun 'git hop doctor --fix' to resolve these issues")
				os.Exit(1)
			}
			output.Fatal("Failed to create worktree: %v", err)
		}

		// Execute post-worktree-add hook
		if err := hookRunner.ExecuteHookWithDetector("post-worktree-add", worktreePath, repoID, branch, detectorEnv); err != nil {
			output.Warn("Hook post-worktree-add failed: %v", err)
		}

		// Register in Hopspace
		if err := hopspace.RegisterBranch(branch, worktreePath); err != nil {
			output.Fatal("Failed to register branch in hopspace: %v", err)
		}

		// Add to Hub
		if err := hub.AddBranch(branch, branch, worktreePath); err != nil {
			output.Fatal("Failed to add branch to hub: %v", err)
		}

		// Update global state
		st, err := state.LoadState(fs)
		if err != nil {
			st = state.NewState()
		}

		// Ensure repository exists in state
		if st.Repositories[repoID] == nil {
			st.AddRepository(repoID, &state.RepositoryState{
				URI:           hub.Config.Repo.URI,
				Org:           hub.Config.Repo.Org,
				Repo:          hub.Config.Repo.Repo,
				DefaultBranch: hub.Config.Repo.DefaultBranch,
				Worktrees:     make(map[string]*state.WorktreeState),
				Hubs:          []*state.HubState{},
			})

			// Add the hub to state
			st.AddHub(repoID, &state.HubState{
				Path:         hubPath,
				Mode:         "local",
				CreatedAt:    time.Now(),
				LastAccessed: time.Now(),
			})
		}

		// Add worktree to state
		if err := st.AddWorktree(repoID, branch, &state.WorktreeState{
			Path:         worktreePath,
			Type:         "linked",
			HubPath:      hubPath,
			CreatedAt:    time.Now(),
			LastAccessed: time.Now(),
		}); err != nil {
			output.Error("Failed to update state: %v", err)
		} else {
			if err := state.SaveState(fs, st); err != nil {
				output.Error("Failed to save state: %v", err)
			}
		}

		// Generate Environment
		// We need to load ports and volumes config
		portsLoader := config.NewLoader(fs)
		portsCfg, err := portsLoader.LoadPortsConfig(hopspacePath)
		if err != nil {
			// Create default configuration if missing
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

		// Check if docker environment exists before trying to generate environment
		hasDockerEnv := d.HasDockerEnv(worktreePath)

		var branchPorts *config.BranchPorts
		var branchVols *config.BranchVolumes

		if hasDockerEnv {
			envMgr := services.NewEnvManager(fs, portsCfg, volsCfg, d)
			branchPorts, branchVols, _, err = envMgr.Generate(branch, worktreePath, hub.Config.Repo.Org, hub.Config.Repo.Repo)
			if err != nil {
				output.Error("Failed to generate environment: %v", err)
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
		}

		// Setup dependencies
		output.Info("Setting up dependencies...")
		depsManager, err := services.NewDepsManager(fs, hopspacePath, globalConfig)
		if err != nil {
			output.Warn("Failed to initialize dependency manager: %v", err)
		} else {
			if err := depsManager.EnsureDeps(worktreePath, branch); err != nil {
				output.Warn("Failed to ensure dependencies: %v", err)
			} else {
				output.Info("Dependencies installed.")
			}
		}

		// Update current symlink to point to new worktree
		if err := hop.UpdateCurrentSymlink(fs, hubPath, worktreePath); err != nil {
			// Don't fail on symlink error, just warn
			output.Warn("Failed to update current symlink: %v", err)
		}

		output.Info("Created hopspace for '%s'", branch)

		relPath, _ := filepath.Rel(cwd, worktreePath)
		if !strings.HasPrefix(relPath, ".") && !filepath.IsAbs(relPath) {
			relPath = "./" + relPath
		}
		output.Info("Worktree: %s", relPath)

		// If running inside an AI coding agent, hint how to add the worktree directory
		if hint := agentDirHint(worktreePath); hint != "" {
			output.Info("Run: %s", hint)
		} else if os.Getenv("OPENCODE") == "1" {
			opencodeAgentHint(worktreePath, os.Stdin)
		}

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

// agentDirHint returns a slash command hint for adding the worktree directory
// to the current session when running inside a supported AI coding agent.
func agentDirHint(path string) string {
	switch {
	case os.Getenv("CLAUDE_CODE") == "1":
		return fmt.Sprintf("/add-dir %s", path)
	case os.Getenv("GEMINI_CLI") == "1":
		return fmt.Sprintf("/directory add %s", path)
	case os.Getenv("COPILOT_GH") == "true":
		return fmt.Sprintf("/add-dir %s", path)
	default:
		return ""
	}
}

// opencodeAgentHint prompts the user to choose local or global OpenCode config,
// then prints the external_directories snippet to add the worktree path.
func opencodeAgentHint(path string, in *os.File) {
	xdgConfig, err := xdg.ConfigDir("opencode")
	if err != nil {
		xdgConfig = filepath.Join(".config", "opencode")
	}
	localCfg := ".opencode/opencode.jsonc"
	globalCfg := filepath.Join(xdgConfig, "opencode.jsonc")

	fmt.Fprintf(os.Stderr, "Add %s to OpenCode config. Which?\n  1) local  (%s)\n  2) global (%s)\nChoice [1/2]: ", path, localCfg, globalCfg)

	reader := bufio.NewReader(in)
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	var cfgPath string
	switch choice {
	case "2":
		cfgPath = globalCfg
	default:
		cfgPath = localCfg
	}

	fmt.Fprintf(os.Stderr, "\nAdd to %s:\n\n  \"external_directories\": [\"%s\"]\n\n", cfgPath, path)
}

func init() {
	cli.RootCmd.AddCommand(addCmd)
	addCmd.ValidArgsFunction = completeRemoteBranchNames
}
