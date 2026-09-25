package hop

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/output"
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

	// EnforceStartPoint makes the start-point binding for a branch that
	// already exists locally (see reconcileExistingBranch) instead of the
	// existing branch being linked as-is. Set it when the user named the
	// start-point explicitly; configured defaults only seed new branches.
	EnforceStartPoint bool

	// Detach makes CreateWorktree check out the start-point on a detached
	// HEAD and leave the branch uncreated, for a caller that creates the
	// branch inside the new worktree (git flow start).
	Detach bool
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
		URI:      hopspaceURI(hopspace),
	}
	worktreePath := ExpandWorktreeLocation(locationPattern, ctx)

	// Clean the path to resolve .. and . segments
	worktreePath = filepath.Clean(worktreePath)

	// Step 2: Pre-flight validation; a preview runs the same checks.
	// Whatever occupies the path is refused, never cleared: only an
	// empty directory is reused, as `git worktree add` reuses it.
	if err := m.CheckAdd(hopspace, hubPath, branch, worktreePath); err != nil {
		return worktreePath, err
	}
	if err := m.CheckStartPoint(hopspace, hubPath, branch, startPoint); err != nil {
		return worktreePath, err
	}

	// Step 3: Call existing CreateWorktree method to do the actual work
	_, err := m.CreateWorktree(hopspace, hubPath, branch, locationPattern, org, repo, defaultBranch, startPoint)
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
//     falling back to refs/heads/<defaultBranch>, then "HEAD".
//   - "default-branch"      → same as "".
//   - "initial"             → root commit of the current history (legacy behavior).
//   - any other value       → passed through verbatim as the start-point ref/SHA.
//
// When the resolved start-point is non-empty and not the literal "HEAD", the
// upstream tracking shortcut is suppressed: the explicit start-point becomes
// the positional <commit-ish> for `git worktree add -b`. Existing branches
// (already present in the repo) are linked rather than re-created, and
// startPoint is irrelevant for that path unless EnforceStartPoint is set.
// A branch that exists only as origin/<branch> counts as existing: it is
// created from, and tracks, the origin branch (see remoteOnlyBranch).
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

	wb := m.findBase(hopspace, hubPath)
	baseWorktreePath := wb.base

	// Expand worktree location pattern
	dataHome := GetGitHopDataHome()
	ctx := WorktreeLocationContext{
		HubPath:  hubPath,
		Branch:   branch,
		Org:      org,
		Repo:     repo,
		DataHome: dataHome,
		URI:      hopspaceURI(hopspace),
	}
	worktreePath := ExpandWorktreeLocation(locationPattern, ctx)

	// Anything but an empty directory at the path is in the way; an
	// empty one is reused, as `git worktree add` reuses it.
	if occupied(m.fs, worktreePath) {
		return worktreePath, fmt.Errorf("worktree already exists at %s", worktreePath)
	}

	// Resolve the effective start-point. The wrapper consumes either a
	// trackBranch (origin/<defaultBranch>) OR a positional base — never both.
	// When the caller (or the resolved default) names a concrete ref, we
	// suppress trackBranch so the explicit start-point wins.
	resolvedBase, suppressTrack := m.resolveStartPoint(baseWorktreePath, startPoint, defaultBranch)

	if m.Detach {
		commit := pinStartPoint(m.git, baseWorktreePath, wb.addDir, resolvedBase)
		if _, err := m.git.RunInDir(wb.addDir, "git", "worktree", "add", "--detach", worktreePath, commit); err != nil {
			return "", fmt.Errorf("failed to create worktree: %w", err)
		}
		return worktreePath, nil
	}

	trackBranch := ""
	if !suppressTrack && defaultBranch != "" {
		trackBranch = "origin/" + defaultBranch
	}

	forceCreate := false
	if m.EnforceStartPoint {
		branchExists, err := m.reconcileExistingBranch(baseWorktreePath, branch, resolvedBase)
		if err != nil {
			return "", err
		}
		forceCreate = !branchExists
	} else if remote := m.remoteOnlyBranch(baseWorktreePath, branch); remote != "" {
		resolvedBase, forceCreate = remote, true
	}
	if wb.addDir != baseWorktreePath {
		resolvedBase = pinStartPoint(m.git, baseWorktreePath, wb.addDir, resolvedBase)
	}
	if err := m.git.CreateWorktree(wb.addDir, branch, worktreePath, resolvedBase, forceCreate, trackBranch); err != nil {
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
		output.Warn("could not resolve default branch %q to a ref; falling back to HEAD",
			defaultBranch)
		return "HEAD", false
	case StartPointInitial:
		out, err := m.git.RunInDir(basePath, "git", "rev-list", "--max-parents=0", "HEAD")
		if err != nil {
			output.Warn("could not resolve root commit (--from initial): %v; falling back to HEAD",
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

// CheckMove reports why MoveWorktree would refuse to rename oldBranch to
// newBranch in hub, moving its worktree to newPath. It only reads the hub
// config, git refs and what is at newPath, so callers can settle it
// before running anything with side effects.
//
// Like add, move refuses a file or a non-empty directory at newPath:
// `git worktree move` would put the worktree inside such a directory,
// while hop.json recorded the directory itself. An empty directory is
// taken (MoveWorktree removes it first).
func CheckMove(fs afero.Fs, hub *Hub, g git.GitInterface, oldBranch, newBranch, newPath string) error {
	if oldBranch == "" || newBranch == "" {
		return fmt.Errorf("branch names cannot be empty")
	}
	if oldBranch == hub.Config.Repo.DefaultBranch {
		return fmt.Errorf("cannot move the default branch '%s'", oldBranch)
	}
	entry, exists := hub.Config.Branches[oldBranch]
	if !exists {
		return fmt.Errorf("branch '%s' not found in hub", oldBranch)
	}
	if _, exists := hub.Config.Branches[newBranch]; exists {
		return fmt.Errorf("branch '%s' already exists", newBranch)
	}
	// An existing newBranch is adopted only when the worktree already has
	// it checked out (the branch was renamed outside git-hop); any other
	// branch by that name is unrelated, and git branch -m would refuse it.
	oldPath := config.ResolveWorktreePath(entry.Path, hub.Path)
	if g.LocalBranchExists(oldPath, newBranch) {
		if cur, err := g.GetCurrentBranch(oldPath); err != nil || cur != newBranch {
			return fmt.Errorf("branch '%s' already exists and is not checked out in '%s'", newBranch, oldBranch)
		}
	}
	if occupied(fs, newPath) {
		return fmt.Errorf("'%s' already exists and is not an empty directory\n"+
			"hint: move it away, or pick another branch name", newPath)
	}
	return nil
}

// MoveWorktree renames a worktree: renames the git branch, moves the directory,
// and updates hub and hopspace configs.
// Returns (oldPath, newPath, error).
func (m *WorktreeManager) MoveWorktree(hopspace *Hopspace, hub *Hub, oldBranch, newBranch string, locationPattern, org, repo string) (string, string, error) {
	// Compute new path from location pattern
	dataHome := GetGitHopDataHome()
	ctx := WorktreeLocationContext{
		HubPath:  hub.Path,
		Branch:   newBranch,
		Org:      org,
		Repo:     repo,
		DataHome: dataHome,
		URI:      hub.Config.Repo.URI,
	}
	newPath := filepath.Clean(ExpandWorktreeLocation(locationPattern, ctx))

	if err := CheckMove(m.fs, hub, m.git, oldBranch, newBranch, newPath); err != nil {
		return "", "", err
	}
	oldPath := config.ResolveWorktreePath(hub.Config.Branches[oldBranch].Path, hub.Path)

	// `git worktree move` into an existing directory, even an empty one,
	// puts the worktree inside it; the empty one CheckMove let through
	// goes first, so the worktree lands at newPath itself. The removal
	// fails on a directory something was put in since.
	if err := NewCleanupManager(m.fs, m.git).RemoveEmptyDirectory(newPath); err != nil {
		return oldPath, newPath, fmt.Errorf("failed to move worktree: %w", err)
	}

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

// hopspaceURI returns the origin URL hopspace records, "" when there is
// no hopspace or config to read it from.
func hopspaceURI(hopspace *Hopspace) string {
	if hopspace == nil || hopspace.Config == nil {
		return ""
	}
	return hopspace.Config.Repo.URI
}
