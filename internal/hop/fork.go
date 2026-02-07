package hop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/git"
	"github.com/jadb/git-hop/internal/output"
	"github.com/jadb/git-hop/internal/state"
	"github.com/spf13/afero"
)

// ForkAttach handles "Fork-Attach Mode" (git hop <uri> --branch <branch>)
func ForkAttach(fs afero.Fs, g git.GitInterface, uri, branch, hubPath string) error {
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
	org, repo := ParseRepoFromURL(uri)
	if org == "" || repo == "" {
		return fmt.Errorf("could not parse org/repo from URI: %s", uri)
	}

	dataHome := GetGitHopDataHome()
	forkHopspacePath := GetHopspacePath(dataHome, org, repo)

	output.Info("Attaching fork branch %s from %s...", branch, uri)
	output.Info("Fork Hopspace: %s", forkHopspacePath)

	// 3. Fork Detection / Validation
	// We need to verify that the remote branch shares history with our local compare branch.
	// Initialize the fork hopspace directory
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
			// With bare repo + worktree structure, b.Path is the full worktree path
			// No symlinks are used in this structure
			worktreePath := b.Path
			// Verify the worktree exists
			if _, err := os.Stat(worktreePath); err == nil {
				output.Info("Found candidate main repo at %s (branch %s)", worktreePath, name)
				mainRepoPath = worktreePath
				break
			} else {
				output.Info("Worktree not found for %s at %s: %v", name, worktreePath, err)
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
	_, err = g.RunInDir(mainRepoPath, "git", "fetch", uri, branch)
	if err != nil {
		return fmt.Errorf("failed to fetch fork branch: %v", err)
	}

	// Determine compare branch (local default or configured)
	compareBranch := hub.Config.Settings.CompareBranch
	if compareBranch == nil {
		// Use current HEAD or default
		// Default to comparing against HEAD
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

	// 4. Create worktree in hub's hops directory
	// Name: <branch>-fork-<org>
	forkBranchName := fmt.Sprintf("%s-fork-%s", branch, org)
	forkWorktreePath := filepath.Join(hubPath, "hops", forkBranchName)

	// We need to add a worktree from the fork hopspace.
	// Since the branch is already checked out there, we need to:
	// 1. Add a remote to the main repo pointing to the fork
	// 2. Fetch from the fork
	// 3. Create a worktree tracking the fork branch

	// Create a detached worktree from the fork branch commit
	// (Git won't allow checking out the same branch twice)

	// Get the commit hash from the fork branch
	sourceWorktreePath := filepath.Join(forkHopspacePath, branch)
	commitHash, err := g.RunInDir(sourceWorktreePath, "git", "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("failed to get commit hash from fork: %v", err)
	}
	commitHash = strings.TrimSpace(commitHash)

	// Create a detached worktree at that commit in the main repo
	// We use the main repo worktree as the base
	if _, err := g.RunInDir(mainRepoPath, "git", "worktree", "add", "--detach", forkWorktreePath, commitHash); err != nil {
		return fmt.Errorf("failed to add fork worktree: %v", err)
	}

	// 5. Update Hub Config
	hub.Config.Branches[forkBranchName] = config.HubBranch{
		Path:           forkWorktreePath,
		HopspaceBranch: branch,
		Fork:           &org,
	}

	writer := config.NewWriter(fs)
	if err := writer.WriteHubConfig(hubPath, hub.Config); err != nil {
		return fmt.Errorf("failed to update hub config: %v", err)
	}

	// Update global state
	st, err := state.LoadState(fs)
	if err != nil {
		st = state.NewState()
	}

	// Get the main repo ID
	mainRepoID := fmt.Sprintf("github.com/%s/%s", hub.Config.Repo.Org, hub.Config.Repo.Repo)

	// Ensure repository exists in state
	if st.Repositories[mainRepoID] == nil {
		st.AddRepository(mainRepoID, &state.RepositoryState{
			URI:           hub.Config.Repo.URI,
			Org:           hub.Config.Repo.Org,
			Repo:          hub.Config.Repo.Repo,
			DefaultBranch: hub.Config.Repo.DefaultBranch,
			Worktrees:     make(map[string]*state.WorktreeState),
			Hubs:          []*state.HubState{},
		})
	}

	// Add fork worktree to state
	if err := st.AddWorktree(mainRepoID, forkBranchName, &state.WorktreeState{
		Path:         forkWorktreePath,
		Type:         "linked",
		HubPath:      hubPath,
		CreatedAt:    time.Now(),
		LastAccessed: time.Now(),
	}); err != nil {
		output.Warn("Failed to update state: %v", err)
	} else {
		if err := state.SaveState(fs, st); err != nil {
			output.Warn("Failed to save state: %v", err)
		}
	}

	output.Info("Successfully attached fork branch as %s", forkBranchName)
	return nil
}
