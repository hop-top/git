package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/config"
	"hop.top/git/internal/docker"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
	"hop.top/git/internal/state"
	"hop.top/git/internal/tui"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

var (
	statusAll bool
)

var statusCmd = &cobra.Command{
	Use:     "status",
	Aliases: []string{"st", "info"},
	Short:   "Show the working tree status",
	Long: `Show the status of the current worktree or hub.

By default, shows status for the current context (worktree or hub).
Use --all to show system-wide git-hop information including all repositories,
configuration, and resource usage.`,
	Run: func(cmd *cobra.Command, args []string) {
		fs := afero.NewOsFs()
		g := git.New()
		d := docker.New()

		cwd, err := os.Getwd()
		if err != nil {
			output.Fatal("Failed to get current directory: %v", err)
		}

		// If --all flag is set, show system-wide status
		if statusAll {
			showSystemStatus(fs, d)
			return
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
			showHubStatus(fs, g, hubPath)
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

func showHubStatus(fs afero.Fs, g git.GitInterface, path string) {
	hub, err := hop.LoadHub(fs, path)
	if err != nil {
		output.Fatal("Failed to load hub: %v", err)
	}

	output.Info("Hub: %s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)
	output.Info("Location: %s", hub.Path)

	defaultBranch := hub.Config.Repo.DefaultBranch
	t := tui.NewTable([]interface{}{"Branch", "State", "Status", "Path"})
	for name, b := range hub.Config.Branches {
		state := "Missing"
		status := "-"
		resolvedPath := config.ResolveWorktreePath(b.Path, hub.Path)
		if _, err := fs.Stat(resolvedPath); err == nil {
			state = "Linked"
			status = getBranchSyncStatus(g, resolvedPath, name, defaultBranch)
		}
		t.AddRow(name, state, status, resolvedPath)
	}
	t.Render()
}

// getBranchSyncStatus reports a worktree's branch position relative to
// the hub's default branch: "default", "merged", "synced",
// "<n> ahead", "<n> behind", or "diverged (<n> ahead, <m> behind)".
// Returns "unknown" when git can't compute the comparison (e.g. the
// default branch ref is missing in this worktree).
func getBranchSyncStatus(g git.GitInterface, dir, branch, defaultBranch string) string {
	if defaultBranch == "" || branch == defaultBranch {
		return "default"
	}

	// rev-list --left-right --count <branch>...<default>
	// Output: "<ahead>\t<behind>"
	out, err := g.RunInDir(dir, "git", "rev-list", "--left-right", "--count",
		branch+"..."+defaultBranch)
	if err != nil {
		return "unknown"
	}

	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) != 2 {
		return "unknown"
	}

	ahead, behind := fields[0], fields[1]
	switch {
	case ahead == "0" && behind == "0":
		return "synced"
	case ahead == "0":
		return fmt.Sprintf("merged (%s behind)", behind)
	case behind == "0":
		return fmt.Sprintf("%s ahead", ahead)
	default:
		return fmt.Sprintf("diverged (%s ahead, %s behind)", ahead, behind)
	}
}

func showWorktreeStatus(fs afero.Fs, g git.GitInterface, d *docker.Docker, path string) {
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
		// Resolve hop-scoped compose project name from the enclosing hub.
		// Falls back to the empty project (compose default) if the worktree
		// isn't inside a known hub, which preserves prior behavior.
		var project string
		if status != nil {
			if hubPath, err := hop.FindHub(fs, root); err == nil {
				if hub, err := hop.LoadHub(fs, hubPath); err == nil {
					project = services.ComposeProjectName(hub.Config.Repo.Org, hub.Config.Repo.Repo, status.Branch)
				}
			}
		}
		ps, err := d.ComposePs(root, project)
		if err != nil {
			output.Error("Failed to get service status: %v", err)
		} else {
			// Print raw docker compose ps output
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
		project := services.ComposeProjectName(hub.Config.Repo.Org, hub.Config.Repo.Repo, branch.HopspaceBranch)
		ps, err := d.ComposePs(fullPath, project)
		if err == nil {
			fmt.Println(ps)
		}
	} else {
		output.Info("  No services defined")
	}
}

func calculateDirSize(fs afero.Fs, path string) (int64, error) {
	var size int64

	err := afero.Walk(fs, path, func(walkPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})

	return size, err
}

func showSystemStatus(fs afero.Fs, d *docker.Docker) {
	// Load state
	st, err := loadStateOrLegacy(fs)
	if err != nil {
		output.Fatal("Failed to load state: %v", err)
	}

	if output.CurrentMode != output.ModeHuman {
		// Simple output for non-human modes
		showSystemStatusPlain(fs, d, st)
		return
	}

	// Enhanced output for human mode
	fmt.Println(output.SimpleHeader("Git-Hop System Status"))
	fmt.Println()

	// Configuration section
	dataHome := hop.GetGitHopDataHome()
	configHome := hop.GetConfigHome()
	configPath := filepath.Join(configHome, "git-hop", "config.json")

	configInfo := output.Section(output.IconConfig, "Configuration", []string{
		output.RenderKeyValue("Data Home", output.RenderPath(dataHome)),
		output.RenderKeyValue("Config", output.RenderPath(configPath)),
		output.RenderKeyValue("Version", "git-hop"),
	})
	fmt.Println(configInfo)

	// Calculate resource statistics
	totalWorktrees := 0
	activeWorktrees := 0
	missingWorktrees := 0
	totalDiskUsage := int64(0)

	for _, repo := range st.Repositories {
		for _, wt := range repo.Worktrees {
			totalWorktrees++
			exists, _ := afero.DirExists(fs, wt.Path)
			if exists {
				activeWorktrees++
				// Calculate disk usage for worktree directory
				size, _ := calculateDirSize(fs, wt.Path)
				totalDiskUsage += size
			} else {
				missingWorktrees++
			}
		}
	}

	// Resources section
	diskUsageStr := formatBytes(totalDiskUsage)
	if totalDiskUsage == 0 {
		diskUsageStr = "unknown"
	}

	resourceInfo := output.Section(output.IconPackage, "Resources", []string{
		output.RenderKeyValue("Repositories", fmt.Sprintf("%d", len(st.Repositories))),
		output.RenderKeyValue("Total Worktrees", fmt.Sprintf("%d", totalWorktrees)),
		output.RenderKeyValue("Active", output.Colorize(fmt.Sprintf("%d", activeWorktrees), "success")),
		output.RenderKeyValue("Missing", output.Colorize(fmt.Sprintf("%d", missingWorktrees), "warning")),
		output.RenderKeyValue("Disk Usage", diskUsageStr),
	})
	fmt.Println(resourceInfo)

	// Count running environments
	runningServices := 0
	activeVolumes := 0

	// Count running services by checking docker-compose status
	for _, repo := range st.Repositories {
		for branch, wt := range repo.Worktrees {
			composePath := filepath.Join(wt.Path, "docker-compose.yml")
			if exists, _ := afero.Exists(fs, composePath); exists {
				// Check if services are running
				project := services.ComposeProjectName(repo.Org, repo.Repo, branch)
				if ps, err := d.ComposePs(wt.Path, project); err == nil && composePsHasRunning(ps) {
					runningServices++
				}
			}
		}
	}

	// Environment section
	envInfo := output.Section(output.IconDocker, "Environment", []string{
		output.RenderKeyValue("Running Services", fmt.Sprintf("%d", runningServices)),
		output.RenderKeyValue("Port Range", "11500-11520"),
		output.RenderKeyValue("Active Volumes", fmt.Sprintf("%d", activeVolumes)),
	})
	fmt.Println(envInfo)

	// Repositories section with tree
	if len(st.Repositories) > 0 {
		var repoLines []string
		repoLines = append(repoLines, "")

		// Sort repos for consistent output
		var repoIDs []string
		for repoID := range st.Repositories {
			repoIDs = append(repoIDs, repoID)
		}
		sortRepoIDs(repoIDs)

		for i, repoID := range repoIDs {
			repo := st.Repositories[repoID]
			isLast := i == len(repoIDs)-1

			// Count running services for this repo
			repoRunning := 0
			for branch, wt := range repo.Worktrees {
				composePath := filepath.Join(wt.Path, "docker-compose.yml")
				if exists, _ := afero.Exists(fs, composePath); exists {
					project := services.ComposeProjectName(repo.Org, repo.Repo, branch)
					if ps, err := d.ComposePs(wt.Path, project); err == nil && composePsHasRunning(ps) {
						repoRunning++
					}
				}
			}

			// Shorten repo ID for display
			shortRepo := repoID
			if len(shortRepo) > 40 {
				shortRepo = "..." + shortRepo[len(shortRepo)-37:]
			}

			statusIcon := output.IconStopped
			statusText := "stopped"
			if repoRunning > 0 {
				statusIcon = output.IconRunning
				statusText = "running"
			}

			line := fmt.Sprintf("%-42s  %d worktrees  %s %d %s",
				shortRepo,
				len(repo.Worktrees),
				output.Colorize(statusIcon, statusText),
				repoRunning,
				statusText,
			)

			repoLines = append(repoLines, output.TreeItem(isLast, line, ""))
		}

		repoInfo := output.Section(output.IconRepo, "Repositories", repoLines)
		fmt.Println(repoInfo)
	}

	// Summary
	fmt.Println()
	if totalWorktrees == 0 {
		fmt.Println(output.StyleMuted.Render("No worktrees found. Run 'git hop <uri>' to clone a repository."))
	} else {
		summary := fmt.Sprintf("Tracking %d worktrees across %d repositories",
			totalWorktrees, len(st.Repositories))
		if runningServices > 0 {
			summary += output.Colorize(fmt.Sprintf(" · %d services running", runningServices), "success")
		}
		fmt.Println(summary)
	}
}

func showSystemStatusPlain(fs afero.Fs, d *docker.Docker, st *state.State) {
	dataHome := hop.GetGitHopDataHome()
	configHome := hop.GetConfigHome()
	configPath := filepath.Join(configHome, "git-hop", "config.json")

	output.Info("Configuration:")
	output.Info("  Data Home: %s", dataHome)
	output.Info("  Config: %s", configPath)
	output.Info("")

	totalWorktrees := 0
	activeWorktrees := 0
	for _, repo := range st.Repositories {
		for _, wt := range repo.Worktrees {
			totalWorktrees++
			if exists, _ := afero.DirExists(fs, wt.Path); exists {
				activeWorktrees++
			}
		}
	}

	output.Info("Resources:")
	output.Info("  Repositories: %d", len(st.Repositories))
	output.Info("  Total Worktrees: %d", totalWorktrees)
	output.Info("  Active: %d", activeWorktrees)
	output.Info("")

	output.Info("Repositories:")
	for repoID, repo := range st.Repositories {
		output.Info("  %s: %d worktrees", repoID, len(repo.Worktrees))
	}
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func sortRepoIDs(ids []string) {
	// Simple bubble sort for small lists
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[i] > ids[j] {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
}

// composePsHasRunning reports whether the output of
// `docker compose ps --format json` represents at least one container.
// Compose emits either a JSON array (older releases) or JSON Lines (one
// object per line, newer releases). The empty cases we must reject are:
//   - the empty string / whitespace
//   - a literal "[]" (empty array, which is 2 characters and would otherwise
//     trip a naive len(ps) > 0 check)
//   - JSON Lines output with no non-empty lines
func composePsHasRunning(ps string) bool {
	trimmed := strings.TrimSpace(ps)
	if trimmed == "" || trimmed == "[]" {
		return false
	}
	// JSON array form: "[ {...}, {...} ]"
	if strings.HasPrefix(trimmed, "[") {
		var arr []any
		if err := json.Unmarshal([]byte(trimmed), &arr); err == nil {
			return len(arr) > 0
		}
		// Malformed JSON array — fall through and treat the non-empty,
		// non-"[]" string as running to avoid false negatives.
		return true
	}
	// JSON Lines form: one object per line.
	for _, line := range strings.Split(trimmed, "\n") {
		if strings.TrimSpace(line) != "" {
			return true
		}
	}
	return false
}

func init() {
	statusCmd.Flags().BoolVar(&statusAll, "all", false, "Show system-wide git-hop status")
	cli.RootCmd.AddCommand(statusCmd)
}
