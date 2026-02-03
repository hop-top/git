package hop

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/git"
	"github.com/jadb/git-hop/internal/output"
	"github.com/spf13/afero"
)

// ForkAttach handles "Fork-Attach Mode" (git hop <uri> --branch <branch>)
func ForkAttach(fs afero.Fs, g *git.Git, uri, branch, hubPath string) error {
	// 1. Validate Hub
	if !IsHub(fs, hubPath) {
		return fmt.Errorf("not in a git-hop hub")
	}

	hub, err := LoadHub(fs, hubPath)
	if err != nil {
		return fmt.Errorf("failed to load hub: %v", err)
	}

	// 2. Determine Fork Hopspace
	// $GIT_HOP_DATA_HOME/<fork-org>/<fork-repo>/
	org, repo := parseURI(uri)
	if org == "" || repo == "" {
		return fmt.Errorf("could not parse org/repo from URI: %s", uri)
	}

	dataHome := GetGitHopDataHome()
	forkHopspacePath := GetHopspacePath(dataHome, org, repo)

	output.Info("Attaching fork branch %s from %s...", branch, uri)
	output.Info("Fork Hopspace: %s", forkHopspacePath)

	// 3. Fork Detection / Validation
	// We need to verify that the remote branch shares history with our local compare branch.
	// First, we need a git context. We can use the Hub's main worktree (if it exists) or any existing worktree.
	// Or we can use the fork hopspace if we initialize it.

	// Let's initialize the fork hopspace first (empty dir)
	if err := os.MkdirAll(forkHopspacePath, 0755); err != nil {
		return fmt.Errorf("failed to create fork hopspace: %v", err)
	}

	// We need to fetch the remote branch to check ancestry.
	// But where do we fetch it TO?
	// We can fetch it into the Hub's main repo if it's a bare repo, or any worktree.
	// The Hub itself is just a directory of symlinks.
	// The "Main Repo" is likely in $HOPSPACE/<default>.
	// We should find the "Main Repo" from the Hub config.

	mainRepoPath := ""
	output.Info("Searching for main repo in %d branches...", len(hub.Config.Branches))
	for name, b := range hub.Config.Branches {
		if b.Fork == nil { // Not a fork, likely part of main repo
			// Resolve symlink
			linkPath := filepath.Join(hubPath, b.Path)
			target, err := os.Readlink(linkPath)
			if err == nil {
				output.Info("Found candidate main repo at %s (branch %s) -> %s", linkPath, name, target)
				mainRepoPath = target
				break
			} else {
				output.Info("Failed to read link for %s: %v", name, err)
			}
		} else {
			output.Info("Skipping fork branch %s", name)
		}
	}

	if mainRepoPath == "" {
		// Fallback: try to find any valid git repo in hub
		return fmt.Errorf("could not find main repository worktree to perform ancestry check")
	}

	// Fetch the fork branch into the main repo as a temporary remote
	// git fetch <uri> <branch>
	// We use FETCH_HEAD to compare.
	output.Info("Verifying fork ancestry in %s...", mainRepoPath)

	// Fetch
	// We can use a temporary remote name or just fetch by URI
	// git fetch <uri> <branch>
	_, err = g.Runner.RunInDir(mainRepoPath, "git", "fetch", uri, branch)
	if err != nil {
		return fmt.Errorf("failed to fetch fork branch: %v", err)
	}

	// Determine compare branch (local default or configured)
	compareBranch := hub.Config.Settings.CompareBranch
	if compareBranch == nil {
		// Use current HEAD or default
		// For now, use HEAD
		cb := "HEAD"
		compareBranch = &cb
	}

	// Check merge-base
	// git merge-base HEAD FETCH_HEAD
	_, err = g.MergeBase(mainRepoPath, *compareBranch, "FETCH_HEAD")
	if err != nil {
		return fmt.Errorf("fork validation failed: branch %s from %s does not share history with %s (use --force to override)", branch, uri, *compareBranch)
	}

	output.Info("Fork ancestry verified.")

	// Now proceed to create worktree in fork hopspace
	// We can clone/add.
	// Since we verified it, we can now add it.
	// If fork hopspace is empty, we can clone.

	isEmpty, _ := afero.IsEmpty(fs, forkHopspacePath)
	if isEmpty {
		output.Info("Initializing fork hopspace...")
		worktreePath := filepath.Join(forkHopspacePath, branch)
		if err := g.Clone(uri, worktreePath, branch); err != nil {
			return fmt.Errorf("failed to clone fork branch: %v", err)
		}

		// Initialize Hopspace Config
		hsCfg := &config.HopspaceConfig{
			Repo: config.RepoConfig{
				URI:           uri,
				Org:           org,
				Repo:          repo,
				DefaultBranch: branch,
			},
			Branches: make(map[string]config.HopspaceBranch),
		}
		hsCfg.Branches[branch] = config.HopspaceBranch{
			Path:     worktreePath,
			LastSync: time.Now(),
			Exists:   true,
		}

		writer := config.NewWriter(fs)
		if err := writer.WriteHopspaceConfig(forkHopspacePath, hsCfg); err != nil {
			return fmt.Errorf("failed to write fork hopspace config: %v", err)
		}
	} else {
		// Fork hopspace exists, add worktree
		forkHopspace, err := LoadHopspace(fs, forkHopspacePath)
		if err != nil {
			return fmt.Errorf("failed to load fork hopspace: %v", err)
		}

		wm := NewWorktreeManager(fs, g)
		// For forks, the hopspace path acts as the hub path (worktrees are stored in hopspace)
		locationPattern := "{hubPath}/hops/{branch}"
		worktreePath, err := wm.CreateWorktree(forkHopspace, forkHopspacePath, branch, locationPattern, forkHopspace.Config.Repo.Org, forkHopspace.Config.Repo.Repo)
		if err != nil {
			return fmt.Errorf("failed to create worktree in fork: %v", err)
		}

		if err := forkHopspace.RegisterBranch(branch, worktreePath); err != nil {
			return fmt.Errorf("failed to register branch in fork hopspace: %v", err)
		}
	}

	// 4. Create Symlink in Hub
	// Name: <branch>-fork-<org>
	symlinkName := fmt.Sprintf("%s-fork-%s", branch, org)
	worktreePath := filepath.Join(forkHopspacePath, branch)

	if err := os.Symlink(worktreePath, filepath.Join(hubPath, symlinkName)); err != nil {
		return fmt.Errorf("failed to create symlink in hub: %v", err)
	}

	// 5. Update Hub Config
	hub.Config.Branches[symlinkName] = config.HubBranch{
		Path:           symlinkName,
		HopspaceBranch: branch,
		Fork:           &org,
	}

	writer := config.NewWriter(fs)
	if err := writer.WriteHubConfig(hubPath, hub.Config); err != nil {
		return fmt.Errorf("failed to update hub config: %v", err)
	}

	output.Info("Successfully attached fork branch as %s", symlinkName)
	return nil
}
