package hop

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/output"
	"hop.top/git/internal/repoid"
	"hop.top/git/internal/state"
)

// ForkAttachment is what ForkAttach added to the hub: the hub branch the
// fork's branch was attached as, and its worktree.
type ForkAttachment struct {
	Branch string
	Path   string
}

// ForkAttach handles "Fork-Attach Mode" (git hop <uri> --branch <branch>)
func ForkAttach(fs afero.Fs, g git.GitInterface, uri, branch, hubPath string) (ForkAttachment, error) {
	// 1. Validate Hub
	if !IsHub(fs, hubPath) {
		return ForkAttachment{}, fmt.Errorf("not in a git-hop hub")
	}

	hub, err := LoadHub(fs, hubPath)
	if err != nil {
		return ForkAttachment{}, fmt.Errorf("failed to load hub: %v", err)
	}

	// 2. Determine Fork Hopspace
	// $GIT_HOP_DATA_HOME/<hop.dataLayout for the fork>/
	org, repo := ParseRepoFromURL(uri)
	if org == "" || repo == "" {
		return ForkAttachment{}, fmt.Errorf("could not parse org/repo from URI: %s", uri)
	}

	dataHome := GetGitHopDataHome()
	forkHopspacePath := GetHopspacePath(dataHome, NewRepoRef(uri, org, repo).In(hubPath))

	output.Info("Attaching fork branch %s from %s...", branch, uri)
	output.Info("Fork Hopspace: %s", forkHopspacePath)

	// 3. Fork Detection / Validation
	// We need to verify that the remote branch shares history with our local compare branch.
	// Initialize the fork hopspace directory
	if err := fs.MkdirAll(forkHopspacePath, 0755); err != nil {
		return ForkAttachment{}, fmt.Errorf("failed to create fork hopspace: %v", err)
	}

	// We need to fetch the remote branch to check ancestry.
	// But where do we fetch it TO?
	// We can fetch it into the Hub's main repo if it's a bare repo, or any worktree.
	// The Hub itself is just a directory of symlinks.
	// The "Main Repo" is likely in $HOPSPACE/<default>.
	// We should find the "Main Repo" from the Hub config.

	mainRepoPath := ""
	output.Info("Searching for main repo in %d branches...", len(hub.Config.Branches))
	worktreePaths := hub.WorktreePaths()
	names := make([]string, 0, len(worktreePaths))
	for name := range worktreePaths {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		b := hub.Config.Branches[name]
		if b.Fork == nil { // Not a fork, likely part of main repo
			// hop.json records paths absolute (add) or hub-relative
			// (init); WorktreePaths resolves both against the hub. Only
			// a worktree directory qualifies: a file at the path is not
			// a repo to fetch into.
			worktreePath := worktreePaths[name]
			presence := WorktreeAt(fs, worktreePath)
			if presence == WorktreePresent {
				output.Info("Found candidate main repo at %s (branch %s)", worktreePath, name)
				mainRepoPath = worktreePath
				break
			}
			if presence == WorktreeOccupied {
				output.Info("Worktree path for %s is not a directory: %s", name, worktreePath)
			} else {
				output.Info("Worktree not found for %s at %s", name, worktreePath)
			}
		} else {
			output.Info("Skipping fork branch %s", name)
		}
	}

	if mainRepoPath == "" {
		// Fallback: try to find any valid git repo in hub
		return ForkAttachment{}, fmt.Errorf("could not find main repository worktree to perform ancestry check")
	}

	// Fetch the fork branch into the main repo as a temporary remote
	// git fetch <uri> <branch>
	// We use FETCH_HEAD to compare.
	output.Info("Verifying fork ancestry in %s...", mainRepoPath)

	// Fetch
	// We can use a temporary remote name or just fetch by URI
	// git fetch <uri> <branch>
	_, err = g.RunInDir(mainRepoPath, "git", git.FetchArgs(uri, branch)...)
	if err != nil {
		return ForkAttachment{}, fmt.Errorf("failed to fetch fork branch: %v", err)
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
		return ForkAttachment{}, &AttachRefusal{
			Msg:  fmt.Sprintf("fork validation failed: branch %s from %s does not share history with %s", branch, uri, *compareBranch),
			Hint: fmt.Sprintf("only a fork of this hub's repository (%s) can be attached; check the URI and --branch", hub.Config.Repo.URI),
		}
	}

	output.Info("Fork ancestry verified.")

	// The hub's worktree of the fork's branch: <branch>-fork-<org>. One
	// already there from an earlier attach is checked before anything
	// changes, so a refusal leaves the fork's hopspace untouched too.
	forkBranchName := fmt.Sprintf("%s-fork-%s", branch, org)
	forkWorktreePath := filepath.Join(hubPath, "hops", forkBranchName)
	existingHub, err := checkHubForkWorktree(fs, g, mainRepoPath, forkWorktreePath)
	if err != nil {
		return ForkAttachment{}, err
	}

	// Now proceed to create worktree in fork hopspace
	// We can clone/add.
	// Since we verified it, we can now add it.
	// If fork hopspace is empty, we can clone.

	// sourceWorktreePath is where the fork's branch is checked out in the
	// fork hopspace: the first branch is a clone at <hopspace>/<branch>,
	// later ones are worktrees wherever CreateWorktree puts them. The hub's
	// worktree is created from its HEAD, so it must be the path actually
	// created, never one rebuilt from the branch name.
	var sourceWorktreePath string
	isEmpty, _ := afero.IsEmpty(fs, forkHopspacePath)
	if isEmpty {
		output.Info("Initializing fork hopspace...")
		worktreePath := filepath.Join(forkHopspacePath, branch)
		if err := g.Clone(uri, worktreePath, branch); err != nil {
			return ForkAttachment{}, fmt.Errorf("failed to clone fork branch: %v", err)
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

		err := WithHopJSONLock(fs, forkHopspacePath, func() error {
			return config.NewWriter(fs).WriteHopspaceConfig(forkHopspacePath, hsCfg)
		})
		if err != nil {
			return ForkAttachment{}, fmt.Errorf("failed to write fork hopspace config: %v", err)
		}
		sourceWorktreePath = worktreePath
	} else {
		// Fork hopspace exists, add worktree
		forkHopspace, err := LoadHopspace(fs, forkHopspacePath)
		if err != nil {
			return ForkAttachment{}, fmt.Errorf("failed to load fork hopspace: %v", err)
		}

		// The fork hopspace is a single-branch clone of the fork's first
		// attached branch: fetch this one so the worktree below starts
		// from the fork's branch rather than a new branch off the first.
		base := NewWorktreeManager(fs, g).findBaseWorktree(forkHopspace, forkHopspacePath)
		refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch)
		if _, err := g.RunInDir(base, "git", git.FetchArgs("origin", refspec)...); err != nil {
			return ForkAttachment{}, fmt.Errorf("failed to fetch fork branch into fork hopspace: %v", err)
		}

		// An earlier attach of this branch, whole or interrupted, left its
		// worktree here: bring it to the fork's branch instead of adding
		// a second one, which git refuses.
		worktreePath := existingForkWorktree(fs, forkHopspace, forkHopspacePath, branch)
		if worktreePath != "" {
			if err := reuseForkWorktree(g, base, worktreePath, branch); err != nil {
				return ForkAttachment{}, err
			}
		} else {
			wm := NewWorktreeManager(fs, g)
			// For forks, the hopspace path acts as the hub path (worktrees are stored in hopspace)
			locationPattern := "{hubPath}/hops/{branch}"
			worktreePath, err = wm.CreateWorktree(forkHopspace, forkHopspacePath, branch, locationPattern, forkHopspace.Config.Repo.Org, forkHopspace.Config.Repo.Repo, forkHopspace.Config.Repo.DefaultBranch, "")
			if err != nil {
				return ForkAttachment{}, fmt.Errorf("failed to create worktree in fork: %v", err)
			}
		}

		if err := forkHopspace.RegisterBranch(branch, worktreePath); err != nil {
			return ForkAttachment{}, fmt.Errorf("failed to register branch in fork hopspace: %v", err)
		}
		sourceWorktreePath = worktreePath
	}

	// 4. Create worktree in hub's hops directory, or bring the one an
	// earlier attach created to the fork's commit.
	// We need to add a worktree from the fork hopspace.
	// Since the branch is already checked out there, we need to:
	// 1. Add a remote to the main repo pointing to the fork
	// 2. Fetch from the fork
	// 3. Create a worktree tracking the fork branch

	// Create a detached worktree from the fork branch commit
	// (Git won't allow checking out the same branch twice)

	// Get the commit hash from the fork branch
	commitHash, err := g.RunInDir(sourceWorktreePath, "git", "rev-parse", "HEAD")
	if err != nil {
		return ForkAttachment{}, fmt.Errorf("failed to get commit hash from fork: %v", err)
	}
	commitHash = strings.TrimSpace(commitHash)

	if existingHub != nil {
		if err := updateHubForkWorktree(g, mainRepoPath, existingHub, commitHash); err != nil {
			return ForkAttachment{}, err
		}
	} else if _, err := g.RunInDir(mainRepoPath, "git", "worktree", "add", "--detach", forkWorktreePath, commitHash); err != nil {
		// Create a detached worktree at that commit in the main repo
		return ForkAttachment{}, fmt.Errorf("failed to add fork worktree: %v", err)
	}

	// 5. Update Hub Config
	err = hub.Update(func(cfg *config.HubConfig) error {
		cfg.Branches[forkBranchName] = config.HubBranch{
			Path:           forkWorktreePath,
			HopspaceBranch: branch,
			Fork:           &org,
		}
		return nil
	})
	if err != nil {
		return ForkAttachment{}, fmt.Errorf("failed to update hub config: %v", err)
	}
	attached := ForkAttachment{Branch: forkBranchName, Path: forkWorktreePath}

	// Update global state
	// A state file that cannot be read is not replaced.
	mainRepoID := repoid.For(hubPath, hub.Config.Repo)
	// The hub the fork's worktree belongs to, when state does not have it.
	mode := state.HubModeLocal
	if hub.Config.Repo.Mode == config.RepoModeGlobal {
		mode = state.HubModeGlobal
	}
	if err := state.Update(fs, func(st *state.State) error {
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
		_ = st.AddHub(mainRepoID, &state.HubState{Path: hubPath, Mode: mode, CreatedAt: time.Now(), LastAccessed: time.Now()})
		// A re-attach keeps when the worktree was created, read from the
		// state Update just loaded under its lock.
		createdAt := time.Now()
		if _, prev, ok := st.Repositories[mainRepoID].WorktreeAt(forkWorktreePath); ok && prev != nil && !prev.CreatedAt.IsZero() {
			createdAt = prev.CreatedAt
		}
		return st.PutWorktree(mainRepoID, &state.WorktreeState{
			Path:         forkWorktreePath,
			Branch:       forkBranchName,
			Type:         "linked",
			HubPath:      hubPath,
			CreatedAt:    createdAt,
			LastAccessed: time.Now(),
		})
	}); err != nil {
		output.Warn("Failed to update state: %v", err)
	}

	output.Info("Successfully attached fork branch as %s", forkBranchName)
	return attached, nil
}
