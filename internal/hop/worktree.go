package hop

import (
	"fmt"
	"path/filepath"

	"github.com/jadb/git-hop/internal/git"
	"github.com/spf13/afero"
)

// WorktreeManager handles worktree operations
type WorktreeManager struct {
	git *git.Git
	fs  afero.Fs
}

// NewWorktreeManager creates a new manager
func NewWorktreeManager(fs afero.Fs, g *git.Git) *WorktreeManager {
	return &WorktreeManager{
		git: g,
		fs:  fs,
	}
}

// CreateWorktree creates a git worktree in the hopspace
func (m *WorktreeManager) CreateWorktree(hopspace *Hopspace, branch string) (string, error) {
	worktreePath := filepath.Join(hopspace.Path, branch)

	// Check if already exists
	if exists, _ := afero.Exists(m.fs, worktreePath); exists {
		return worktreePath, fmt.Errorf("worktree already exists at %s", worktreePath)
	}

	// git worktree add
	// Note: We need to run this from the hopspace root or the main repo.
	// Ideally, the hopspace IS the main repo (bare or non-bare).
	// But in git-hop, the hopspace contains worktrees.
	// Wait, the specs say:
	// $GIT_HOP_DATA_HOME/<org>/<repo>/
	//     hop.json
	//     <branch>/
	//
	// So where is the .git directory?
	// Clone mode:
	// git clone --branch <default> --single-branch <uri> $HOPSPACE/<default>
	//
	// So the first worktree IS the repo? Or is it a bare repo?
	// "Create the first worktree" implies the first one is special or it's a bare repo with worktrees.
	//
	// If we use `git clone`, we get a standard repo.
	// Subsequent worktrees must be added from *that* repo.
	//
	// If we use a bare repo at $HOPSPACE, then all branches are worktrees off it.
	//
	// Let's assume for now that $HOPSPACE/<default> is the "main" worktree/repo.
	// And other worktrees are created from it.
	//
	// Actually, `git worktree add` needs to be run from an existing worktree or the main repo.
	//
	// If we are in Clone Mode:
	// 1. Clone default branch to $HOPSPACE/<default>
	//
	// If we are adding a new branch:
	// We need to find a valid git context in the hopspace.
	// $HOPSPACE/<default> is a safe bet.

	// Find a valid base for git commands
	basePath := ""
	if len(hopspace.Config.Branches) > 0 {
		// Pick any existing branch
		for _, b := range hopspace.Config.Branches {
			if b.Exists {
				basePath = b.Path
				break
			}
		}
	}

	if basePath == "" {
		// No branches? This shouldn't happen if initialized correctly.
		// Unless it's a bare repo setup.
		// For now, let's assume we have at least one.
		return "", fmt.Errorf("no existing worktrees found in hopspace to derive from")
	}

	if err := m.git.WorktreeAdd(basePath, branch, worktreePath); err != nil {
		// If failed, maybe branch doesn't exist? Try creating it.
		// We assume failure means "invalid reference".
		// We use HEAD as base for now.
		if err2 := m.git.WorktreeAddCreate(basePath, branch, worktreePath, "HEAD"); err2 != nil {
			return "", fmt.Errorf("failed to add worktree (and failed to create branch): %v / %v", err, err2)
		}
	}

	return worktreePath, nil
}

// RemoveWorktree removes a worktree
func (m *WorktreeManager) RemoveWorktree(hopspace *Hopspace, branch string) error {
	worktreePath := filepath.Join(hopspace.Path, branch)

	// We need a base path that is NOT the one we are removing
	basePath := ""
	for bName, b := range hopspace.Config.Branches {
		if bName != branch && b.Exists {
			basePath = b.Path
			break
		}
	}

	if basePath == "" {
		// If this is the last worktree, we might just be deleting the folder?
		// But `git worktree remove` requires a git context.
		// If it's the main worktree, we can't remove it with `worktree remove`.
		// We have to just delete the dir?
		//
		// If it's the main repo (cloned one), we can't remove it via worktree remove.
		//
		// For now, let's try `git worktree remove`.
		// If it fails because it's the main one, we might need to handle it differently.
		// But typically we shouldn't remove the main one unless we are nuking the whole hopspace.
		return fmt.Errorf("cannot remove the last/main worktree via this method")
	}

	if err := m.git.WorktreeRemove(basePath, worktreePath, true); err != nil {
		return err
	}

	return nil
}
