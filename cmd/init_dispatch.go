package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/config"
	"hop.top/git/internal/docker"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
)

// initRepoID resolves the 3-part repo ID ("host/org/repo") init uses for
// the hopspace: org/repo from the remote URL, falling back to the local
// path (matches registerAsIs). Returns "" when neither yields a name.
func initRepoID(g git.GitInterface, repoPath string) string {
	org, repo := "", ""
	if g != nil {
		if remoteURL, err := g.GetRemoteURL(repoPath); err == nil && remoteURL != "" {
			org, repo = hop.ParseRepoFromURL(remoteURL)
		}
	}
	if org == "" || repo == "" {
		abs, err := filepath.Abs(repoPath)
		if err == nil {
			repo = filepath.Base(abs)
			org = filepath.Base(filepath.Dir(abs))
		}
	}
	if org == "" || repo == "" {
		return ""
	}
	return fmt.Sprintf("github.com/%s/%s", org, repo)
}

// dispatchInitWorktreeAdd fires post-worktree-add for the conversion's
// initial worktree (see initWorktreePath), through the same dispatch clone
// uses. A failing hook warns and does not undo the conversion, matching
// clone.
func dispatchInitWorktreeAdd(fs afero.Fs, g git.GitInterface, repoPath, worktreePath, branch string) {
	repoID := initRepoID(g, repoPath)
	if err := cli.BuildHookDispatch(fs).PostWorktreeAdd(worktreePath, repoID, branch); err != nil {
		output.Warn("post-worktree-add hook failed: %v", err)
	}
}

// setUpInitWorktree prepares the conversion's initial worktree (see
// initWorktreePath) through the same path as add and clone
// (services.SetUpWorktree): ports, volumes, .env and compose override,
// then shared deps, publishing deps.installed when it linked them. Init
// runs it before post-worktree-add, so the hook finds them. It is not a
// hook, so --no-hooks does not skip it. Like add, it never fails the
// conversion.
func setUpInitWorktree(fs afero.Fs, hub *hop.Hub, repoPath, worktreePath, branch string) {
	if hub == nil || worktreePath == "" {
		return
	}
	loader := config.NewGlobalLoader()
	globalConfig, err := loader.Load()
	if err != nil {
		globalConfig = loader.GetDefaults()
	}
	target := services.EnvTarget{
		Root:         worktreePath,
		Branch:       branch,
		HopspacePath: hop.ResolveHopspacePath(repoPath, hub.Config.Repo),
		Hub:          hub.Config,
	}
	services.SetUpWorktree(fs, docker.New(), target, globalConfig).
		PublishDepsInstalled(context.Background(), cli.EventBus)
}

// initWorktreePath is the working tree a conversion leaves the current
// branch in: hops/<branch> for a bare conversion, the repo root itself for
// a regular one.
func initWorktreePath(repoPath, branch string, useBare bool) string {
	if !useBare {
		return repoPath
	}
	return filepath.Join(repoPath, "hops", branch)
}

// previewInitWorktreeAdd reports the post-worktree-add hook a conversion
// would dispatch for its initial worktree, without running it.
func previewInitWorktreeAdd(fs afero.Fs, g git.GitInterface, repoPath, branch string, useBare bool) {
	if branch == "" {
		return
	}
	worktreePath := initWorktreePath(repoPath, branch, useBare)
	cli.PreviewHook(hooks.NewRunner(fs), "post-worktree-add", worktreePath, initRepoID(g, repoPath))
}

// initHooksHintWidth caps each hint line so the list wraps like the
// surrounding init output.
const initHooksHintWidth = 72

// initHooksHint is the advice printed under the hooks-dir message: which
// hooks a script in that directory can implement, derived from the runner
// so it only names hooks that fire and are looked up there.
//
// inWorktree means the directory sits inside a worktree (bare conversion:
// hops/<branch>/) rather than at the hub root. Some hooks never start
// their lookup at a worktree (see hooks.HubOnlyHookNames); those are not
// offered there, and the hint names the hub's hooks dir for them instead.
func initHooksHint(hubPath string, inWorktree bool) string {
	names := hooks.RepoLevelHookNames()
	if inWorktree {
		names = hooks.WorktreeLevelHookNames()
	}
	lines := []string{"Place executable scripts there to hook into git-hop operations:"}
	lines = append(lines, hookListLines(names)...)
	if inWorktree {
		lines = append(lines,
			"A worktree's hooks directory is not searched for the hooks below;",
			"place them in "+filepath.Join(hubPath, ".git-hop", "hooks")+"/ instead:")
		lines = append(lines, hookListLines(hooks.HubOnlyHookNames())...)
	}
	return strings.Join(lines, "\n")
}

// hookListLines formats hook names as indented, comma-separated lines no
// wider than initHooksHintWidth. A pre-/post- pair is folded into
// "pre/post-<op>".
func hookListLines(names []string) []string {
	have := make(map[string]bool, len(names))
	for _, n := range names {
		have[n] = true
	}

	var items []string
	for _, n := range names {
		switch {
		case strings.HasPrefix(n, "pre-") && have["post-"+strings.TrimPrefix(n, "pre-")]:
			items = append(items, "pre/post-"+strings.TrimPrefix(n, "pre-"))
		case strings.HasPrefix(n, "post-") && have["pre-"+strings.TrimPrefix(n, "post-")]:
			// Folded into its pre- counterpart.
		default:
			items = append(items, n)
		}
	}

	var lines []string
	line := ""
	for i, item := range items {
		if i < len(items)-1 {
			item += ","
		}
		if line != "" && len(line)+1+len(item) > initHooksHintWidth {
			lines = append(lines, line)
			line = ""
		}
		if line == "" {
			line = "  " + item
		} else {
			line += " " + item
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// printInitHooksHint prints initHooksHint under the hooks-dir message.
func printInitHooksHint(hubPath string, inWorktree bool) {
	output.Hint("%s", initHooksHint(hubPath, inWorktree))
}
