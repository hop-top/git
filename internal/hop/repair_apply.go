package hop

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
)

// Applier executes Plan actions against a hub. Each apply method returns
// (changed, err): changed=true when the action actually mutated state,
// false when it was a NoOp; err propagates the first failure.
//
// Applier stops at the first failure; callers do not invoke subsequent
// actions when err != nil.
type Applier struct {
	fs  afero.Fs
	git git.GitInterface
}

// NewApplier constructs an Applier.
func NewApplier(fs afero.Fs, g git.GitInterface) *Applier {
	return &Applier{fs: fs, git: g}
}

// Apply runs every Action in plan in order. It returns the count of
// actions that mutated state and the first error encountered (or nil).
//
// hubPath is needed because some applier paths invoke git inside the hub.
func (a *Applier) Apply(plan *Plan) (int, error) {
	mutations := 0
	for i := range plan.Actions {
		changed, err := a.applyOne(plan.HubPath, &plan.Actions[i])
		if err != nil {
			return mutations, fmt.Errorf("action %d (%s on %s): %w",
				i, plan.Actions[i].Kind, plan.Actions[i].WorktreePath, err)
		}
		if changed {
			mutations++
		}
	}
	return mutations, nil
}

func (a *Applier) applyOne(hubPath string, action *Action) (bool, error) {
	switch action.Kind {
	case ActionNoOp:
		return false, nil
	case ActionRewriteGitdir:
		return a.rewriteGitdir(hubPath, action)
	case ActionRegisterWithGit:
		return a.registerWithGit(hubPath, action)
	case ActionUnregisterFromGit:
		return a.unregisterFromGit(hubPath, action)
	case ActionUpdateHopJSON:
		return a.updateHopJSON(hubPath, action)
	default:
		return false, fmt.Errorf("unknown action kind %d", action.Kind)
	}
}

// rewriteGitdir delegates to `git worktree repair` from the hub. Git's
// repair walks every worktree's .git pointer and rewrites stale ones —
// idempotent and safe to run when only one is broken.
//
// Post-condition: the .git pointer's gitdir target now exists on disk.
func (a *Applier) rewriteGitdir(hubPath string, action *Action) (bool, error) {
	if err := a.git.WorktreeRepair(hubPath); err != nil {
		return false, fmt.Errorf("git worktree repair: %w", err)
	}
	if err := verifyGitdirHealthy(a.fs, action.WorktreePath); err != nil {
		return true, fmt.Errorf("post-action verify: %w", err)
	}
	return true, nil
}

// registerWithGit adds an existing on-disk worktree to git's registry.
// We use `git worktree add <path> <branch>` (linking mode), assuming the
// branch already exists since the directory was created by an earlier
// `git hop add`.
func (a *Applier) registerWithGit(hubPath string, action *Action) (bool, error) {
	branch := branchFromHubByPath(a.fs, hubPath, action.WorktreePath)
	if branch == "" {
		return false, fmt.Errorf("cannot determine branch for %s from hop.json", action.WorktreePath)
	}
	if err := a.git.WorktreeAdd(hubPath, branch, action.WorktreePath); err != nil {
		return false, fmt.Errorf("git worktree add: %w", err)
	}
	out, err := a.git.WorktreeListPorcelain(hubPath)
	if err != nil {
		return true, fmt.Errorf("post-action verify (list): %w", err)
	}
	if !pathInRegistered(action.WorktreePath, parsePorcelainWorktrees(out)) {
		return true, fmt.Errorf("post-action verify: %s not in registry after add", action.WorktreePath)
	}
	return true, nil
}

// unregisterFromGit prunes git's registry of stale entries. `git worktree
// prune` removes administrative files for worktrees whose directories
// have been deleted — exactly the action we want here.
func (a *Applier) unregisterFromGit(hubPath string, action *Action) (bool, error) {
	if err := a.git.WorktreePrune(hubPath); err != nil {
		return false, fmt.Errorf("git worktree prune: %w", err)
	}
	out, err := a.git.WorktreeListPorcelain(hubPath)
	if err != nil {
		return true, fmt.Errorf("post-action verify (list): %w", err)
	}
	if pathInRegistered(action.WorktreePath, parsePorcelainWorktrees(out)) {
		return true, fmt.Errorf("post-action verify: %s still registered after prune", action.WorktreePath)
	}
	return true, nil
}

// updateHopJSON realigns hop.json with reality. Two sub-cases:
//
//   - hop.json references a path that does not exist on disk: drop the entry.
//   - git registry has a worktree hop.json doesn't know about: add the entry.
//
// Branch name resolution falls back to the directory basename when the
// porcelain output is unavailable, since registering arbitrary git
// worktrees that pre-date hop is best-effort.
func (a *Applier) updateHopJSON(hubPath string, action *Action) (bool, error) {
	hub, err := LoadHub(a.fs, hubPath)
	if err != nil {
		return false, fmt.Errorf("load hub: %w", err)
	}
	exists, _ := afero.DirExists(a.fs, action.WorktreePath)

	branchInHub := branchKeyForPath(hub, hubPath, action.WorktreePath)
	switch {
	case branchInHub != "" && !exists:
		delete(hub.Config.Branches, branchInHub)
	case branchInHub == "" && exists:
		key := filepath.Base(action.WorktreePath)
		rel, err := filepath.Rel(hubPath, action.WorktreePath)
		if err != nil {
			rel = action.WorktreePath
		}
		hub.Config.Branches[key] = config.HubBranch{Path: rel, HopspaceBranch: key}
	default:
		return false, nil
	}
	if err := hub.Save(); err != nil {
		return true, fmt.Errorf("save hub: %w", err)
	}

	reloaded, err := LoadHub(a.fs, hubPath)
	if err != nil {
		return true, fmt.Errorf("post-action verify (reload): %w", err)
	}
	stillThere := branchKeyForPath(reloaded, hubPath, action.WorktreePath) != ""
	if exists != stillThere {
		return true, fmt.Errorf("post-action verify: hop.json mismatch (exists=%v, present=%v)", exists, stillThere)
	}
	return true, nil
}

// verifyGitdirHealthy checks that wtPath/.git resolves to an existing
// gitdir target. Symmetric with Planner.gitdirStale's negation.
func verifyGitdirHealthy(fs afero.Fs, wtPath string) error {
	gitPointer := filepath.Join(wtPath, ".git")
	info, err := fs.Stat(gitPointer)
	if err != nil {
		return fmt.Errorf("stat .git: %w", err)
	}
	if info.IsDir() {
		return nil
	}
	content, err := afero.ReadFile(fs, gitPointer)
	if err != nil {
		return fmt.Errorf("read .git: %w", err)
	}
	target := trimGitdirLine(string(content))
	if target == "" {
		return fmt.Errorf(".git is not a gitdir pointer")
	}
	if _, err := fs.Stat(target); err != nil {
		return fmt.Errorf("gitdir target %s missing: %w", target, err)
	}
	return nil
}

func trimGitdirLine(s string) string {
	for _, line := range splitLines(s) {
		const prefix = "gitdir:"
		if len(line) > len(prefix) && line[:len(prefix)] == prefix {
			return trimSpace(line[len(prefix):])
		}
	}
	return ""
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\r' || s[0] == '\n') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\r' || s[len(s)-1] == '\n') {
		s = s[:len(s)-1]
	}
	return s
}

// branchFromHubByPath returns the branch key (or hopspaceBranch) whose
// path matches wtPath, or empty if hop.json doesn't reference it.
func branchFromHubByPath(fs afero.Fs, hubPath, wtPath string) string {
	hub, err := LoadHub(fs, hubPath)
	if err != nil {
		return ""
	}
	for name, b := range hub.Config.Branches {
		if absHubBranchPath(hubPath, b.Path) == wtPath {
			if b.HopspaceBranch != "" {
				return b.HopspaceBranch
			}
			return name
		}
	}
	return ""
}

func branchKeyForPath(hub *Hub, hubPath, wtPath string) string {
	for name, b := range hub.Config.Branches {
		if absHubBranchPath(hubPath, b.Path) == wtPath {
			return name
		}
	}
	return ""
}
