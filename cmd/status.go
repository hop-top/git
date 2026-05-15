package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

	t := tui.NewTable([]interface{}{"Branch", "Base", "State", "Status", "Path"})
	for name, b := range hub.Config.Branches {
		state := "Missing"
		status := "-"
		compare := resolveCompareBranch(hub.Config, b)
		resolvedPath := config.ResolveWorktreePath(b.Path, hub.Path)
		if _, err := fs.Stat(resolvedPath); err == nil {
			state = "Linked"
			status = getBranchSyncStatus(g, resolvedPath, name, compare)
		}
		t.AddRow(name, compare, state, status, resolvedPath)
	}
	t.Render()
}

// resolveCompareBranch picks the branch to use as the ahead/behind/merged
// comparison target for a worktree. Precedence:
//   1. HubBranch.Base (per-branch override, recorded at `git hop add` time
//      or back-filled by `git hop repair --base`)
//   2. HubSettings.CompareBranch (hub-wide override, when configured)
//   3. RepoConfig.DefaultBranch (final fallback)
//
// The returned value is the bare branch name. An empty result means the
// hub has no usable comparison target — callers receive "default" from
// getBranchSyncStatus in that case (see its first branch).
func resolveCompareBranch(cfg *config.HubConfig, b config.HubBranch) string {
	if b.Base != nil && *b.Base != "" {
		return *b.Base
	}
	if cfg.Settings.CompareBranch != nil && *cfg.Settings.CompareBranch != "" {
		return *cfg.Settings.CompareBranch
	}
	return cfg.Repo.DefaultBranch
}

// getBranchSyncStatus reports a worktree's branch position relative to
// the hub's default branch: "default", "synced", "merged (N behind)",
// "behind (N)", "N ahead", "diverged (N ahead, M behind)", or "unknown".
//
// The "merged" label is gated on more than commit reachability. rev-list
// alone treats three different histories identically (all produce
// ahead==0): a branch that was actually merged into default via a merge
// commit, an unborn branch with no commits of its own, and a branch
// that was reset back to default. To distinguish the genuine merge case
// from the unborn/reset cases, we additionally require:
//
//  1. The local tracking ref refs/remotes/origin/<branch> is absent
//     (gh pr merge --delete-branch prunes it locally via fetch), AND
//  2. branch.<name>.merge config is present (= the branch was once
//     tracking a remote, so "ref is gone" means "remote deleted",
//     not "never pushed")
//
// When either signal is missing we drop the merge claim and report
// "behind (N)" — honest about position without overclaiming. The
// trade-off: branches merged via merge-commit whose remote was not
// deleted lose the "merged" label here. They remain detectable via
// `git hop remove --merged`, which has its own probe.
//
// Squash- and rebase-merges produce ahead>0 in rev-list (commits have
// different SHAs even when content is equivalent) and fall into the
// "diverged" or "ahead" buckets — except when --delete-branch also
// runs, in which case the absent remote ref is invisible to this
// function (still ahead>0). Detecting those requires content-based
// comparison (git cherry / patch-id) which is out of scope here.
//
// When the worktree has tracked-but-uncommitted edits, staged changes,
// or untracked files, the label is suffixed with ", dirty". "default"
// and "unknown" are never suffixed: the default branch's dirtiness is
// surfaced elsewhere, and "unknown" already signals we couldn't probe.
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
	var label string
	switch {
	case ahead == "0" && behind == "0":
		label = "synced"
	case ahead == "0":
		if branchWasMergedByRemoteDeletion(g, dir, branch) {
			label = fmt.Sprintf("merged (%s behind)", behind)
		} else {
			label = fmt.Sprintf("behind (%s)", behind)
		}
	case behind == "0":
		label = fmt.Sprintf("%s ahead", ahead)
	default:
		label = fmt.Sprintf("diverged (%s ahead, %s behind)", ahead, behind)
	}

	// Probe working-tree state. On any error, fail open (no suffix):
	// the commit-derived label is still useful; we just couldn't prove
	// dirtiness either way.
	if status, err := g.GetStatus(dir); err == nil && !status.Clean {
		label += ", " + formatDirtyDetail(g, dir, status)
	}
	return label
}

// formatDirtyDetail builds the dirty suffix detail block. Returns
// "dirty (T tracked +X/-Y, C unmerged, U untracked)" with only nonzero
// segments included. Falls back to bare "dirty" when no segment can
// be counted (defensive — Status.Clean=false should always have at
// least one categorized file, but the parser could surface unknown
// porcelain prefixes from future git versions).
//
// Line deltas (+X/-Y) come from `git diff --shortstat` (unstaged) and
// `git diff --cached --shortstat` (staged), summed. When both probes
// fail or return empty, the tracked segment drops the delta and shows
// the count alone — readers still see "1 tracked" rather than nothing.
func formatDirtyDetail(g git.GitInterface, dir string, status *git.Status) string {
	tracked, unmerged, untracked := categorizeDirtyFiles(status.Files)
	if tracked == 0 && unmerged == 0 && untracked == 0 {
		return "dirty"
	}

	var segments []string
	if tracked > 0 {
		seg := fmt.Sprintf("%d tracked", tracked)
		if ins, dels, ok := dirtyLineDeltas(g, dir); ok {
			seg += fmt.Sprintf(" +%d/-%d", ins, dels)
		}
		segments = append(segments, seg)
	}
	if unmerged > 0 {
		segments = append(segments, fmt.Sprintf("%d unmerged", unmerged))
	}
	if untracked > 0 {
		segments = append(segments, fmt.Sprintf("%d untracked", untracked))
	}
	return "dirty (" + strings.Join(segments, ", ") + ")"
}

// categorizeDirtyFiles counts porcelain v2 entries by category. Files
// is the slice populated by Git.GetStatus: each entry is the raw
// porcelain line, so the prefix (the first whitespace-separated token)
// is the category key. "1" and "2" are tracked changes (modified,
// staged, renamed, copied); "u" is an unmerged conflict; "?" is
// untracked. Lines that don't match any of these are ignored
// defensively (future porcelain extensions).
func categorizeDirtyFiles(files []string) (tracked, unmerged, untracked int) {
	for _, line := range files {
		if line == "" {
			continue
		}
		// First field only — porcelain v2 prefixes are single chars.
		switch line[:1] {
		case "1", "2":
			tracked++
		case "u":
			unmerged++
		case "?":
			untracked++
		}
	}
	return
}

// dirtyLineDeltas returns total insertions and deletions across
// unstaged and staged diffs, summed. Returns ok=false when both
// probes produce no parseable output (failed probe or genuinely no
// diffable changes — caller drops the delta segment).
func dirtyLineDeltas(g git.GitInterface, dir string) (insertions, deletions int, ok bool) {
	addFromShortstat := func(args ...string) bool {
		out, err := g.RunInDir(dir, "git", args...)
		if err != nil {
			return false
		}
		ins, dels, parsed := parseShortstat(out)
		if !parsed {
			return false
		}
		insertions += ins
		deletions += dels
		return true
	}
	unstagedOK := addFromShortstat("diff", "--shortstat")
	stagedOK := addFromShortstat("diff", "--cached", "--shortstat")
	return insertions, deletions, unstagedOK || stagedOK
}

// parseShortstat extracts insertion and deletion counts from a
// `git diff --shortstat` line. The output shape is one of:
//
//	" N files changed, X insertions(+), Y deletions(-)"
//	" N file changed, X insertions(+)"            (no deletions)
//	" N file changed, Y deletions(-)"             (no insertions)
//	""                                              (no diff)
//
// Singular ("file"/"insertion"/"deletion") and plural forms both
// appear; we match by suffix. Returns ok=false on empty input so the
// caller can tell "no diff" from "0 insertions, 0 deletions".
func parseShortstat(out string) (insertions, deletions int, ok bool) {
	out = strings.TrimSpace(out)
	if out == "" {
		return 0, 0, false
	}
	// Split on commas; first segment is "N files changed" (discarded),
	// the rest carry the counts. Each count segment ends in
	// "insertion(s)(+)" or "deletion(s)(-)".
	for _, seg := range strings.Split(out, ",") {
		seg = strings.TrimSpace(seg)
		fields := strings.Fields(seg)
		if len(fields) < 2 {
			continue
		}
		n, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		switch {
		case strings.HasPrefix(fields[1], "insertion"):
			insertions = n
		case strings.HasPrefix(fields[1], "deletion"):
			deletions = n
		}
	}
	return insertions, deletions, true
}

// branchWasMergedByRemoteDeletion reports whether the branch's local
// tracking ref is gone AND the branch was once configured to track a
// remote. The combination is the local fingerprint left behind by
// `gh pr merge --delete-branch` (and equivalent flows): the PR merge
// deletes the remote branch, the subsequent fetch+prune removes the
// local refs/remotes/origin/<branch> ref, but branch.<name>.merge in
// .git/config persists. Distinguishes a deleted-after-merge branch
// from a never-pushed unborn branch (both have ahead==0).
func branchWasMergedByRemoteDeletion(g git.GitInterface, dir, branch string) bool {
	if _, err := g.RunInDir(dir, "git", "rev-parse", "--verify",
		"refs/remotes/origin/"+branch); err == nil {
		// Remote tracking ref still present — branch may yet be merged
		// (e.g. merge-commit merge without remote-delete), but we can't
		// prove it from this signal alone.
		return false
	}
	out, err := g.RunInDir(dir, "git", "config", "--get",
		"branch."+branch+".merge")
	return err == nil && strings.TrimSpace(out) != ""
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
