package hop

import (
	"fmt"
	"strings"
	"time"

	"hop.top/git/internal/config"
	"hop.top/git/internal/state"
	"github.com/spf13/afero"
)

// MigrateRegistry migrates the old hops registry to the new state structure
func MigrateRegistry(fs afero.Fs, oldRegistry *Registry, newState *state.State) error {
	if oldRegistry == nil || oldRegistry.Config == nil {
		return fmt.Errorf("invalid old registry")
	}

	// Group hops by repository
	grouped := groupHopsByRepo(oldRegistry.Config.Hops)

	// Migrate each repository
	for repoString, hops := range grouped {
		if err := migrateRepository(fs, repoString, hops, newState); err != nil {
			return fmt.Errorf("failed to migrate repository %s: %w", repoString, err)
		}
	}

	return nil
}

// groupHopsByRepo groups hop entries by repository
func groupHopsByRepo(hops map[string]config.HopEntry) map[string][]config.HopEntry {
	grouped := make(map[string][]config.HopEntry)

	for _, hop := range hops {
		repo := hop.Repo
		grouped[repo] = append(grouped[repo], hop)
	}

	return grouped
}

// migrateRepository migrates a single repository and its worktrees
func migrateRepository(fs afero.Fs, repoString string, hops []config.HopEntry, newState *state.State) error {
	// Extract org and repo
	org, repo := extractOrgRepo(repoString)
	if org == "" || repo == "" {
		return fmt.Errorf("invalid repository format: %s", repoString)
	}

	// Create repository ID (assume github.com as default domain)
	repoID := fmt.Sprintf("github.com/%s/%s", org, repo)

	// Find the project root (use the first hop's project root)
	var projectRoot string
	if len(hops) > 0 {
		projectRoot = hops[0].ProjectRoot
		if projectRoot == "" {
			projectRoot = hops[0].Path
		}
	}

	// Create repository state
	repoState := &state.RepositoryState{
		URI:           fmt.Sprintf("git@github.com:%s/%s.git", org, repo),
		Org:           org,
		Repo:          repo,
		DefaultBranch: "main", // Default assumption
		Worktrees:     make(map[string]*state.WorktreeState),
		Hubs:          []*state.HubState{},
		GlobalHopspace: &state.GlobalHopspaceState{
			Enabled: false,
			Path:    nil,
		},
	}

	// Migrate worktrees
	for _, hop := range hops {
		worktreeType := determineWorktreeType(hop.Path, projectRoot)

		worktreeState := &state.WorktreeState{
			Path:         hop.Path,
			Type:         worktreeType,
			HubPath:      projectRoot,
			CreatedAt:    hop.AddedAt,
			LastAccessed: hop.LastSeen,
		}

		repoState.Worktrees[hop.Branch] = worktreeState

		// Update default branch if this is the bare repo
		if worktreeType == "bare" {
			repoState.DefaultBranch = hop.Branch
		}
	}

	// Add hub entry
	if projectRoot != "" {
		hubState := &state.HubState{
			Path:         projectRoot,
			Mode:         "local",
			CreatedAt:    time.Now(),
			LastAccessed: time.Now(),
		}
		repoState.Hubs = append(repoState.Hubs, hubState)
	}

	// Add to new state
	newState.AddRepository(repoID, repoState)

	return nil
}

// extractOrgRepo extracts organization and repository from "org/repo" format
func extractOrgRepo(repoString string) (org string, repo string) {
	parts := strings.Split(repoString, "/")
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

// determineWorktreeType determines if a worktree is bare or linked
func determineWorktreeType(path string, projectRoot string) string {
	if path == projectRoot {
		return "bare"
	}
	return "linked"
}
