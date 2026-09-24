package hop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/output"
)

// HookMirrorOptions describes how committed .git-hop/hooks/ scripts should
// be mirrored into hopspace at the end of clone/init. Carried as an opaque
// struct so callers in cmd/cli can populate it without internal/hop having
// to import internal/hooks (which would create an import cycle: hooks → hop).
type HookMirrorOptions struct {
	// Mode is one of "symlink", "copy", "prompt", "none", or empty (resolve
	// from env/config/default at call time).
	Mode string
	// Overwrite, when true, replaces an existing hopspace hook with
	// different content in symlink/copy modes.
	Overwrite bool
	// Run, when non-nil, is invoked at the end of CloneWorktree with the
	// just-created worktree absolute path and the 3-part repo ID
	// ("host/org/repo"). Callers wire this to hooks.MirrorCommittedHooks.
	// If nil, no mirror is attempted.
	Run func(worktreePath, repoID string) error
}

// HookDispatchOptions carries the lifecycle-hook dispatch callbacks for
// CloneWorktree. Same rationale as HookMirrorOptions.Run: internal/hop
// cannot import internal/hooks (the cycle already runs hooks → hop for
// LooksLikeGitCheckout), so the caller in internal/cli constructs the
// closures over hooks.Runner and injects them here.
//
// Each callback receives the path handed to the hook runner as
// GIT_HOP_WORKTREE_PATH, the 3-part repo ID, and the branch. A nil
// callback means "no dispatch" and is silently skipped.
type HookDispatchOptions struct {
	// PreClone fires before any filesystem work. A non-nil error aborts
	// the clone. It receives the intended project root (not "" and not
	// the caller's cwd) as its path argument, a directory that does not
	// exist yet. The hook runner resolves pre-clone at hopspace and
	// global level only, since there is no repo on disk to hold one.
	PreClone func(path, repoID, branch string) error
	// PostWorktreeAdd fires after the initial worktree exists AND after
	// the committed-hook mirror has run, so a repo-level hook carried by
	// the clone itself applies to the very worktree that carried it. Its
	// path argument is the initial worktree, never the hub root.
	PostWorktreeAdd func(path, repoID, branch string) error
	// PostClone fires last, after the initial worktree is fully
	// registered and SetUpEnv has run. Its path argument is the initial
	// worktree.
	PostClone func(path, repoID, branch string) error
	// SetUpEnv is not a hook: it prepares the initial worktree's
	// environment (ports, volumes, .env, compose override) for the hub at
	// hubPath. It runs after PostWorktreeAdd, as add generates after its
	// own post-worktree-add, and before PostClone, so post-clone sees the
	// environment. It reports its own failures and never fails the clone.
	SetUpEnv func(hubPath string)
}

// CloneWorktree clones uri into a hub at projectPath. The hub is always a
// bare repository with the default branch checked out at
// hops/<defaultBranch>/ (docs/stories/015-hopspace-shape-contract.md).
func CloneWorktree(fs afero.Fs, g git.GitInterface, uri, projectPath string, globalConfig bool, hookOpts HookMirrorOptions, dispatch HookDispatchOptions) error {
	projectRoot := projectPath

	if projectRoot == "" {
		parts := strings.Split(uri, "/")
		repoName := parts[len(parts)-1]
		repoName = strings.TrimSuffix(repoName, ".git")
		cwd, _ := os.Getwd()
		projectRoot = filepath.Join(cwd, repoName)
	}

	// Ensure projectRoot is absolute to avoid path issues with git -C
	absProjectRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %v", err)
	}
	projectRoot = absProjectRoot

	org, repo := ParseRepoFromURL(uri)
	if org == "" || repo == "" {
		return fmt.Errorf("could not parse org/repo from URI: %s", uri)
	}

	// pre-clone fires before any filesystem work, so a non-zero exit
	// aborts before the directory-exists probe and the clone itself. The
	// branch is not known yet (resolving it requires talking to the
	// remote), so the hook sees an empty GIT_HOP_BRANCH. See
	// HookDispatchOptions.PreClone for the path it receives.
	if dispatch.PreClone != nil {
		if err := dispatch.PreClone(projectRoot, repoIDFor(org, repo), ""); err != nil {
			return fmt.Errorf("pre-clone hook aborted clone: %w", err)
		}
	}

	if exists, _ := afero.DirExists(fs, projectRoot); exists {
		return fmt.Errorf("directory already exists: %s", projectRoot)
	}

	defaultBranch, err := g.GetDefaultBranch(uri)
	if err != nil {
		return fmt.Errorf("failed to get default branch: %v", err)
	}

	fmt.Printf("Cloning %s...\n", uri)
	fmt.Printf("Project root: %s\n", projectRoot)
	fmt.Printf("Default branch: %s\n", defaultBranch)

	if err := cloneBareRepo(fs, g, uri, projectRoot, defaultBranch); err != nil {
		return err
	}

	// All worktrees are under hops/ subdirectory (default pattern)
	mainWorktreePath := filepath.Join(projectRoot, "hops", defaultBranch)

	// Ensure we use absolute path for hopspace registration
	absMainWorktreePath, err := filepath.Abs(mainWorktreePath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %v", err)
	}

	// The data home is part of a working install (doctor checks it) even
	// when this hub keeps its hopspace locally and writes nothing there.
	if err := fs.MkdirAll(GetGitHopDataHome(), 0o755); err != nil {
		return fmt.Errorf("failed to create data directory: %v", err)
	}

	if globalConfig {
		// Global mode: separate hub and hopspace configs
		if err := createProjectConfig(fs, projectRoot, uri, org, repo, defaultBranch); err != nil {
			return err
		}

		// Initialize hopspace in data directory
		dataHome := GetGitHopDataHome()
		hopspacePath := GetHopspacePath(dataHome, org, repo)
		if err := initializeHopspace(fs, hopspacePath, uri, org, repo, defaultBranch, absMainWorktreePath, true); err != nil {
			return fmt.Errorf("failed to initialize hopspace: %v", err)
		}
	} else {
		// Local mode (default): merged hub+hopspace config in project root
		if err := createMergedConfig(fs, projectRoot, uri, org, repo, defaultBranch, absMainWorktreePath); err != nil {
			return err
		}
	}

	RegisterNewHub(fs, NewHub{
		URI:           uri,
		Org:           org,
		Repo:          repo,
		DefaultBranch: defaultBranch,
		HubPath:       projectRoot,
		WorktreePath:  absMainWorktreePath,
		WorktreeType:  WorktreeTypeBare,
		Global:        globalConfig,
	})
	repoID := repoIDFor(org, repo)

	// Update current symlink to point to main worktree
	if err := UpdateCurrentSymlink(fs, projectRoot, absMainWorktreePath); err != nil {
		output.Warn("failed to create current symlink: %v", err)
	}

	// Mirror committed .git-hop/hooks/ scripts into the user's hopspace
	// so post-worktree-add (etc.) fires for the very first worktree.
	// Caller wires HookMirrorOptions.Run; we just invoke it here.
	if hookOpts.Run != nil {
		if err := hookOpts.Run(absMainWorktreePath, repoID); err != nil {
			output.Warn("failed to mirror committed hooks: %v", err)
		}
	}

	// ORDERING IS LOAD-BEARING: post-worktree-add fires AFTER the mirror
	// above, which is what makes a committed repo-level hook apply to the
	// very worktree that carried it. Moving this dispatch before the
	// mirror silently drops that hook on the first worktree. The path is
	// the initial worktree, never the hub root.
	if dispatch.PostWorktreeAdd != nil {
		if err := dispatch.PostWorktreeAdd(absMainWorktreePath, repoID, defaultBranch); err != nil {
			output.Warn("post-worktree-add hook failed: %v", err)
		}
	}

	if dispatch.SetUpEnv != nil {
		dispatch.SetUpEnv(projectRoot)
	}

	// post-clone fires last, once the initial worktree is fully
	// registered (state, symlink, mirror, post-worktree-add, environment
	// all done).
	if dispatch.PostClone != nil {
		if err := dispatch.PostClone(absMainWorktreePath, repoID, defaultBranch); err != nil {
			output.Warn("post-clone hook failed: %v", err)
		}
	}

	// Get relative path from projectRoot to mainWorktreePath for display
	relWorktreePath, err := filepath.Rel(projectRoot, mainWorktreePath)
	if err != nil {
		relWorktreePath = mainWorktreePath
	}
	worktreeDir := filepath.Dir(relWorktreePath)

	fmt.Printf("\nSuccessfully cloned to %s\n", projectRoot)
	fmt.Printf("\nProject structure:\n")
	fmt.Printf("  %s/  (bare repository)\n", projectRoot)
	fmt.Printf("    hop.json\n")
	fmt.Printf("    %s/\n", worktreeDir)
	fmt.Printf("      %s/           (worktree for current branch)\n", defaultBranch)

	fmt.Printf("\nYou can now:\n")
	fmt.Printf("  cd %s         # Work on current branch\n", mainWorktreePath)
	fmt.Printf("  git hop add <branch>  # Add new branch\n")
	fmt.Printf("  git hop <branch>      # Jump to worktree\n")
	fmt.Printf("  git hop              # List all worktrees\n")

	return nil
}

// repoIDFor builds the 3-part repo ID ("host/org/repo") that hook
// resolution and state keying both use. Hoisted so pre-clone (which runs
// before the state block) and the later dispatches agree on one value.
func repoIDFor(org, repo string) string {
	return fmt.Sprintf("github.com/%s/%s", org, repo)
}

func cloneBareRepo(fs afero.Fs, g git.GitInterface, uri, projectRoot, defaultBranch string) error {
	fmt.Println("Creating bare repository...")

	if err := g.CloneBare(uri, projectRoot); err != nil {
		return fmt.Errorf("failed to create bare repository: %w", err)
	}

	// `git clone --bare` strips the standard fetch refspec, so
	// `refs/remotes/origin/*` is never populated and downstream calls like
	// setUpstreamTracking fail. Restore the refspec and re-fetch
	// so origin/<defaultBranch> exists locally. Real GitHub URLs sometimes
	// configure this implicitly; local file paths (and some hosts) do not,
	// so do it unconditionally.
	if _, err := g.Run("git", "-C", projectRoot, "config",
		"remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		return fmt.Errorf("failed to set origin fetch refspec: %w", err)
	}
	if _, err := g.Run("git", "-C", projectRoot, "fetch", "origin"); err != nil {
		return fmt.Errorf("failed to fetch origin: %w", err)
	}

	// Create hops directory
	hopsDir := filepath.Join(projectRoot, "hops")
	if err := fs.MkdirAll(hopsDir, 0755); err != nil {
		return fmt.Errorf("failed to create hops directory: %w", err)
	}

	// Create main worktree under hops/
	mainPath := filepath.Join(hopsDir, defaultBranch)
	_, err := g.Run("git", "-C", projectRoot, "worktree", "add", mainPath, defaultBranch)
	if err != nil {
		os.RemoveAll(projectRoot)
		return fmt.Errorf("failed to create main worktree: %w", err)
	}

	if err := setUpstreamTracking(g, mainPath, defaultBranch); err != nil {
		return fmt.Errorf("failed to set upstream tracking: %w", err)
	}

	if err := ensureWorktreeHooksDir(fs, mainPath); err != nil {
		return fmt.Errorf("failed to seed hooks directory: %w", err)
	}

	return nil
}

// setUpstreamTracking sets the upstream tracking branch so first push
// doesn't require --set-upstream.
func setUpstreamTracking(g git.GitInterface, worktreePath, branch string) error {
	_, err := g.RunInDir(worktreePath, "git", "branch", "--set-upstream-to=origin/"+branch, branch)
	return err
}

// ensureWorktreeHooksDir creates the .git-hop/hooks directory inside a
// freshly-created worktree so user-supplied hooks can be dropped in
// without an extra `git hop init` step. Inlined here (rather than going
// through internal/hooks.Runner.InstallHooks) to avoid an import cycle:
// internal/hooks already imports internal/hop for LooksLikeGitCheckout.
// We just created the worktree, so the existence check is unnecessary.
func ensureWorktreeHooksDir(fs afero.Fs, worktreePath string) error {
	hooksDir := filepath.Join(worktreePath, ".git-hop", "hooks")
	return fs.MkdirAll(hooksDir, 0755)
}

func createProjectConfig(fs afero.Fs, projectRoot, uri, org, repo, defaultBranch string) error {
	cfgPath := filepath.Join(projectRoot, "hop.json")

	cfg := map[string]any{
		"repo": map[string]any{
			"uri":           uri,
			"org":           org,
			"repo":          repo,
			"defaultBranch": defaultBranch,
			"structure":     "bare-worktree",
			"isBare":        true,
			"mode":          config.RepoModeGlobal,
		},
		"branches": map[string]any{
			defaultBranch: map[string]any{
				"path":           config.MakeWorktreePath(defaultBranch),
				"hopspaceBranch": defaultBranch,
			},
		},
		"settings": map[string]any{
			"envPatterns": []string{"dev", "staging", "qa"},
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := afero.WriteFile(fs, cfgPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// createMergedConfig creates a single hop.json with both hub and hopspace fields (local mode)
func createMergedConfig(fs afero.Fs, projectRoot, uri, org, repo, defaultBranch, worktreePath string) error {
	cfgPath := filepath.Join(projectRoot, "hop.json")

	cfg := map[string]any{
		"repo": map[string]any{
			"uri":           uri,
			"org":           org,
			"repo":          repo,
			"defaultBranch": defaultBranch,
			"structure":     "bare-worktree",
			"isBare":        true,
		},
		// Hub branches (points to worktree paths - full absolute paths)
		"branches": map[string]any{
			defaultBranch: map[string]any{
				"path":           worktreePath,
				"hopspaceBranch": defaultBranch,
				// Hopspace fields (merged into same branches map)
				"exists":   true,
				"lastSync": time.Now().Format(time.RFC3339),
			},
		},
		"settings": map[string]any{
			"envPatterns": []string{"dev", "staging", "qa"},
		},
		"forks": map[string]any{},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := afero.WriteFile(fs, cfgPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("Created local configuration (hub+hopspace) at %s\n", cfgPath)
	return nil
}

func registerProject(fs afero.Fs, org, repo, branch, worktreePath string) error {
	registry := LoadRegistry(fs)
	repoKey := org + "/" + repo
	if _, ok := registry.Config.Hops[repoKey+":"+branch]; ok {
		// Recorded already, by this hub or another one of the repository:
		// kept as it is, like the state entries RegisterNewHub merges with.
		return nil
	}

	absPath, err := filepath.Abs(worktreePath)
	if err != nil {
		return err
	}

	if err := registry.AddHop(repoKey, branch, absPath); err != nil {
		return err
	}

	output.Info("Registered in global registry: %s:%s", repoKey, branch)

	return nil
}

// initializeHopspace creates the hopspace directory structure and config
func initializeHopspace(fs afero.Fs, hopspacePath, uri, org, repo, defaultBranch, worktreePath string, isGlobal bool) error {
	// Use InitHopspace function which creates the directory and config
	hopspace, err := InitHopspace(fs, hopspacePath, uri, org, repo, defaultBranch)
	if err != nil {
		return fmt.Errorf("failed to initialize hopspace: %w", err)
	}

	// Register the initial branch (main) in the hopspace
	if err := hopspace.RegisterBranch(defaultBranch, worktreePath); err != nil {
		return fmt.Errorf("failed to register default branch: %w", err)
	}

	location := "locally"
	if isGlobal {
		location = "globally"
	}
	fmt.Printf("Initialized hopspace %s at %s\n", location, hopspacePath)
	return nil
}
