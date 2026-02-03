package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jadb/git-hop/internal/cli"
	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/docker"
	"github.com/jadb/git-hop/internal/git"
	"github.com/jadb/git-hop/internal/hop"
	"github.com/jadb/git-hop/internal/output"
	"github.com/jadb/git-hop/internal/tui"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:     "status",
	Aliases: []string{"st", "info"},
	Short:   "Show the working tree status",
	Run: func(cmd *cobra.Command, args []string) {
		fs := afero.NewOsFs()
		g := git.New()
		d := docker.New()

		cwd, err := os.Getwd()
		if err != nil {
			output.Fatal("Failed to get current directory: %v", err)
		}

		if len(args) > 0 {
			target := args[0]
			hubPath, err := hop.FindHub(fs, cwd)
			if err == nil {
				showTargetStatus(fs, d, hubPath, target)
				return
			}
			output.Fatal("Target status only available inside a hub")
		}

		// Check context
		hubPath, err := hop.FindHub(fs, cwd)
		if err == nil {
			showHubStatus(fs, hubPath)
			return
		}

		// Check if inside a worktree
		// We can check if we are in a git worktree
		if g.IsInsideWorkTree(cwd) {
			showWorktreeStatus(fs, g, d, cwd)
			return
		}

		output.Info("Not in a hub or worktree.")
	},
}

func showHubStatus(fs afero.Fs, path string) {
	hub, err := hop.LoadHub(fs, path)
	if err != nil {
		output.Fatal("Failed to load hub: %v", err)
	}

	output.Info("Hub: %s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)
	output.Info("Location: %s", hub.Path)

	t := tui.NewTable([]interface{}{"Branch", "State", "Path"})
	for name, b := range hub.Config.Branches {
		// Check if path exists
		state := "Missing"
		if _, err := fs.Stat(filepath.Join(hub.Path, b.Path)); err == nil {
			state = "Linked"
		}
		t.AddRow(name, state, b.Path)
	}
	t.Render()
}

func showWorktreeStatus(fs afero.Fs, g *git.Git, d *docker.Docker, path string) {
	root, err := g.GetRoot(path)
	if err != nil {
		output.Fatal("Failed to get git root: %v", err)
	}

	// Git Status
	status, err := g.GetStatus(root)
	if err != nil {
		output.Error("Failed to get git status: %v", err)
	} else {
		output.Info("On branch %s", status.Branch)
		if status.Clean {
			output.Info("nothing to commit, working tree clean")
		} else {
			output.Info("Changes not staged for commit:")
			for _, f := range status.Files {
				fmt.Println(f)
			}
		}
	}

	// Docker Status
	// Check if docker-compose.yml exists
	if _, err := fs.Stat(filepath.Join(root, "docker-compose.yml")); err == nil {
		output.Info("\nServices:")
		ps, err := d.ComposePs(root)
		if err != nil {
			output.Error("Failed to get service status: %v", err)
		} else {
			// Parse JSON output or just print?
			// ComposePs returns JSON string.
			// For now, let's just print it or parse it if we want a table.
			// But `docker compose ps` output is complex.
			// Let's just run `docker compose ps` directly for human output if not in porcelain mode?
			// But we wrapped it to return string.
			// Let's just print the raw output for now or improve wrapper to return struct.
			fmt.Println(ps)
		}
	}
}

func showTargetStatus(fs afero.Fs, d *docker.Docker, hubPath, target string) {
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		output.Fatal("Failed to load hub: %v", err)
	}

	branch, ok := hub.Config.Branches[target]
	if !ok {
		output.Fatal("Branch %s not found in hub", target)
	}

	output.Info("Environment: %s", target)
	output.Info("Worktree: %s", branch.Path)

	// Load ports
	dataHome := hop.GetGitHopDataHome()
	hopspacePath := hop.GetHopspacePath(dataHome, hub.Config.Repo.Org, hub.Config.Repo.Repo)

	portsLoader := config.NewLoader(fs)
	portsCfg, _ := portsLoader.LoadPortsConfig(hopspacePath)

	if portsCfg != nil {
		if bp, ok := portsCfg.Branches[branch.HopspaceBranch]; ok && len(bp.Ports) > 0 {
			var minPort, maxPort int
			first := true
			for _, p := range bp.Ports {
				if first || p < minPort {
					minPort = p
				}
				if first || p > maxPort {
					maxPort = p
				}
				first = false
			}
			output.Info("Ports: %d-%d", minPort, maxPort)
		}
	}

	output.Info("\nServices:")
	fullPath := filepath.Join(hubPath, branch.Path)
	if _, err := fs.Stat(filepath.Join(fullPath, "docker-compose.yml")); err == nil {
		ps, err := d.ComposePs(fullPath)
		if err == nil {
			fmt.Println(ps)
		}
	} else {
		output.Info("  No services defined")
	}
}

func init() {
	cli.RootCmd.AddCommand(statusCmd)
}
