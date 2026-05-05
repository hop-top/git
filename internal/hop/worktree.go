package hop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"github.com/spf13/afero"
)

// StartPointInitial is the sentinel string that requests the legacy
// behavior of branching new worktrees from the repository's root commit.
const StartPointInitial = "initial"

// StartPointDefaultBranch is the sentinel string that requests branching
// new worktrees from the tip of repo.defaultBranch (the new default).
const StartPointDefaultBranch = "default-branch"

// WorktreeManager handles worktree operations
type WorktreeManager struct {
	git git.GitInterface
	fs  afero.Fs
}

// NewWorktreeManager creates a new manager
func NewWorktreeManager(fs afero.Fs, g git.GitInterface) *WorktreeManager {
	return &WorktreeManager{
		git: g,
		fs:  fs,
	}
}

// CreateWorktreeTransactional creates a git worktree with validation and auto-cleanup.
// startPoint controls where the new branch begins; see CreateWorktree for resolution rules.
func (m *WorktreeManager) CreateWorktreeTransactional(hopspace *Hopspace, hubPath string, branch string, locationPattern string, org string, repo string, defaultBranch string, startPoint string) (string, error) {
	// Validate inputs early (before path computation)
	if hubPath == "" {
		return "", fmt.Errorf("hubPath cannot be empty")
	}
	if branch == "" {
		return "", fmt.Errorf("branch cannot be empty")
	}

	// Step 1: Expand worktree location using ExpandWorktreeLocation
	dataHome := GetGitHopDataHome()
	ctx := WorktreeLocationContext{
		HubPath:  hubPath,
		Branch:   branch,
		Org:      org,
		Repo:     repo,
		DataHome: dataHome,
	}
	worktreePath := ExpandWorktreeLocation(locationPattern, ctx)

	// Clean the path to resolve .. and . segments
	worktreePath = filepath.Clean(worktreePath)

	// Step 2: Pre-flight validation
	validator := NewStateValidator(m.fs, m.git)
	validation, err := validator.ValidateWorktreeAdd(hopspace, hubPath, branch, worktreePath)
	if err != nil {
		return worktreePath, fmt.Errorf("validation failed: %w", err)
	}

	// Step 3: Auto-cleanup orphaned directories if needed
	if !validation.CanProceed && validation.RequiresCleanup {
		cleanup := NewCleanupManager(m.fs, m.git)
		if err := cleanup.CleanupOrphanedDirectory(worktreePath); err != nil {
			return worktreePath, fmt.Errorf("failed to cleanup orphaned directory: %w", err)
		}
	}

	// Step 4: Call existing CreateWorktree method to do the actual work
	_, err = m.CreateWorktree(hopspace, hubPath, branch, locationPattern, org, repo, defaultBranch, startPoint)
	if err != nil {
		// Return our cleaned path on error
		return worktreePath, err
	}
	// Return our cleaned path on success
	return worktreePath, nil
}

// CreateWorktree creates a git worktree at the configured location.
// startPoint controls the start-point for newly-created branches:
//   - ""                    → resolve to refs/remotes/origin/<defaultBranch>,
//                             falling back to refs/heads/<defaultBranch>, then "HEAD".
//   - "default-branch"      → same as "".
//   - "initial"             → root commit of the current history (legacy behavior).
//   - any other value       → passed through verbatim as the start-point ref/SHA.
//
// When the resolved start-point is non-empty and not the literal "HEAD", the
// upstream tracking shortcut is suppressed: the explicit start-point becomes
// the positional <commit-ish> for `git worktree add -b`. Existing branches
// (already present in the repo) are linked rather than re-created, and
// startPoint is irrelevant for that path.
func (m *WorktreeManager) CreateWorktree(hopspace *Hopspace, hubPath string, branch string, locationPattern string, org string, repo string, defaultBranch string, startPoint string) (string, error) {
	// Validate inputs
	if hubPath == "" {
		return "", fmt.Errorf("hubPath cannot be empty")
	}
	if branch == "" {
		return "", fmt.Errorf("branch cannot be empty")
	}

	// Verify hubPath exists and is a valid git repository
	exists, err := afero.DirExists(m.fs, hubPath)
	if err != nil {
		return "", fmt.Errorf("failed to check hub path: %w", err)
	}
	if !exists {
		return "", fmt.Errorf("hub path does not exist: %s", hubPath)
	}

	// Find an existing worktree in this hub to use as base for git commands
	var baseWorktreePath string

	for _, b := range hopspace.Config.Branches {
		if b.Exists && b.Path != "" {
			// Resolve the path relative to hub if it's not absolute
			branchPath := config.ResolveWorktreePath(b.Path, hubPath)

			// Check if this worktree belongs to the current hub
			if strings.HasPrefix(branchPath, hubPath+string(filepath.Separator)) || strings.HasPrefix(branchPath, hubPath) {
				baseWorktreePath = branchPath
				break
			}
		}
	}

	// If no worktree found in this hub, use any existing worktree
	// (this handles the case where we're adding to a new clone of the same repo)
	if baseWorktreePath == "" {
		for _, b := range hopspace.Config.Branches {
			if b.Exists && b.Path != "" {
				// Resolve the path relative to hub if it's not absolute
				branchPath := config.ResolveWorktreePath(b.Path, hubPath)
				baseWorktreePath = branchPath
				break
			}
		}
	}

	// If no existing worktree found, use the hub path (bare repo) as base
	if baseWorktreePath == "" {
		baseWorktreePath = hubPath
	}

	// Expand worktree location pattern
	dataHome := GetGitHopDataHome()
	ctx := WorktreeLocationContext{
		HubPath:  hubPath,
		Branch:   branch,
		Org:      org,
		Repo:     repo,
		DataHome: dataHome,
	}
	worktreePath := ExpandWorktreeLocation(locationPattern, ctx)

	// Check if already exists
	if exists, _ := afero.Exists(m.fs, worktreePath); exists {
		return worktreePath, fmt.Errorf("worktree already exists at %s", worktreePath)
	}

	// Resolve the effective start-point. The wrapper consumes either a
	// trackBranch (origin/<defaultBranch>) OR a positional base — never both.
	// When the caller (or the resolved default) names a concrete ref, we
	// suppress trackBranch so the explicit start-point wins.
	resolvedBase, suppressTrack := m.resolveStartPoint(baseWorktreePath, startPoint, defaultBranch)

	trackBranch := ""
	if !suppressTrack && defaultBranch != "" {
		trackBranch = "origin/" + defaultBranch
	}
	if err := m.git.CreateWorktree(baseWorktreePath, branch, worktreePath, resolvedBase, false, trackBranch); err != nil {
		return "", fmt.Errorf("failed to create worktree: %w", err)
	}

	return worktreePath, nil
}

// resolveStartPoint maps the caller's startPoint hint to a concrete ref or
// SHA suitable for `git worktree add -b <branch> <path> <commit-ish>`. The
// second return value reports whether the caller's request is specific
// enough to suppress the implicit `--track origin/<defaultBranch>` shortcut.
//
// Resolution rules:
//   - ""                  / "default-branch": probe refs/remotes/origin/<def>, then refs/heads/<def>;
//     fall back to "HEAD" (with a stderr warning) if neither resolves. trackBranch stays
//     active for this case so the new branch tracks origin/<def>.
//   - "initial":           resolve via `git rev-list --max-parents=0 HEAD` (last line); suppress track.
//   - explicit ref/SHA:    pass through unchanged; suppress track.
func (m *WorktreeManager) resolveStartPoint(basePath, startPoint, defaultBranch string) (resolved string, suppressTrack bool) {
	switch startPoint {
	case "", StartPointDefaultBranch:
		if defaultBranch == "" {
			return "HEAD", false
		}
		if _, err := m.git.RevParse(basePath, "--verify", "refs/remotes/origin/"+defaultBranch); err == nil {
			return "refs/remotes/origin/" + defaultBranch, true
		}
		if _, err := m.git.RevParse(basePath, "--verify", "refs/heads/"+defaultBranch); err == nil {
			return "refs/heads/" + defaultBranch, true
		}
		fmt.Fprintf(os.Stderr,
			"warning: could not resolve default branch %q to a ref; falling back to HEAD\n",
			defaultBranch)
		return "HEAD", false
	case StartPointInitial:
		out, err := m.git.RunInDir(basePath, "git", "rev-list", "--max-parents=0", "HEAD")
		if err != nil {
			fmt.Fprintf(os.Stderr,
				"warning: could not resolve root commit (--from initial): %v; falling back to HEAD\n",
				err)
			return "HEAD", true
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) == 0 || lines[len(lines)-1] == "" {
			return "HEAD", true
		}
		return strings.TrimSpace(lines[len(lines)-1]), true
	default:
		// Explicit ref / SHA — pass through unchanged.
		return startPoint, true
	}
}

// MoveWorktree renames a worktree: renames the git branch, moves the directory,
// and updates hub and hopspace configs.
// Returns (oldPath, newPath, error).
func (m *WorktreeManager) MoveWorktree(hopspace *Hopspace, hub *Hub, oldBranch, newBranch string, locationPattern, org, repo string) (string, string, error) {
	if oldBranch == "" || newBranch == "" {
		return "", "", fmt.Errorf("branch names cannot be empty")
	}

	// Guard: cannot move the default branch
	if oldBranch == hub.Config.Repo.DefaultBranch {
		return "", "", fmt.Errorf("cannot move the default branch '%s'", oldBranch)
	}

	// Resolve old path from hub config
	branchCfg, exists := hub.Config.Branches[oldBranch]
	if !exists {
		return "", "", fmt.Errorf("branch '%s' not found in hub", oldBranch)
	}
	oldPath := config.ResolveWorktreePath(branchCfg.Path, hub.Path)

	// Guard: new branch must not already exist
	if _, exists := hub.Config.Branches[newBranch]; exists {
		return oldPath, "", fmt.Errorf("branch '%s' already exists", newBranch)
	}

	// Compute new path from location pattern
	dataHome := GetGitHopDataHome()
	ctx := WorktreeLocationContext{
		HubPath:  hub.Path,
		Branch:   newBranch,
		Org:      org,
		Repo:     repo,
		DataHome: dataHome,
	}
	newPath := filepath.Clean(ExpandWorktreeLocation(locationPattern, ctx))

	// Find a base path for git commands (any other worktree)
	var basePath string
	for bn, bc := range hub.Config.Branches {
		if bn != oldBranch && bc.Path != "" {
			basePath = config.ResolveWorktreePath(bc.Path, hub.Path)
			break
		}
	}
	if basePath == "" {
		basePath = hub.Path
	}

	// 1. Rename git branch (skip if already renamed — e.g. git hop add used newBranch directly)
	if !m.git.LocalBranchExists(basePath, newBranch) {
		if err := m.git.RenameBranch(basePath, oldBranch, newBranch); err != nil {
			return oldPath, newPath, fmt.Errorf("failed to rename branch: %w", err)
		}
	}

	// 2. Move worktree directory
	if err := m.git.WorktreeMove(basePath, oldPath, newPath); err != nil {
		return oldPath, newPath, fmt.Errorf("failed to move worktree: %w", err)
	}

	// 3. Update hub config
	if err := hub.RenameBranch(oldBranch, newBranch, newPath); err != nil {
		return oldPath, newPath, fmt.Errorf("failed to update hub config: %w", err)
	}

	// 4. Update hopspace config
	if err := hopspace.RenameBranch(oldBranch, newBranch, newPath); err != nil {
		return oldPath, newPath, fmt.Errorf("failed to update hopspace config: %w", err)
	}

	return oldPath, newPath, nil
}

// RemoveWorktree removes a worktree
func (m *WorktreeManager) RemoveWorktree(hopspace *Hopspace, branch string) error {
	// Find the worktree to remove
	branchInfo, exists := hopspace.Config.Branches[branch]
	if !exists {
		return fmt.Errorf("branch %s not found in hopspace", branch)
	}

	worktreePath := branchInfo.Path

	// We need a base path that is NOT the one we are removing
	var basePath string
	for bName, b := range hopspace.Config.Branches {
		if bName != branch && b.Exists && b.Path != "" {
			basePath = b.Path
			break
		}
	}

	if basePath == "" {
		// Cannot remove the last worktree - git worktree remove requires a git context from another worktree
		return fmt.Errorf("cannot remove the last/main worktree via this method")
	}

	// Use absolute path for git commands
	absBasePath, err := filepath.Abs(basePath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	if err := m.git.WorktreeRemove(absBasePath, worktreePath, true); err != nil {
		return err
	}

	return nil
}
