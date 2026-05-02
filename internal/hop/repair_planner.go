package hop

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
)

// Planner builds repair plans by classifying each worktree's state.
//
// A worktree is the join of three sources of truth:
//
//  1. on-disk:        does the directory exist; does its .git pointer resolve
//  2. git registry:   does `git worktree list --porcelain` know about it
//  3. hub config:     does hop.json reference it (and at the right path)
//
// Mismatches between these sources translate into Actions.
type Planner struct {
	fs  afero.Fs
	git git.GitInterface
}

// NewPlanner constructs a Planner.
func NewPlanner(fs afero.Fs, g git.GitInterface) *Planner {
	return &Planner{fs: fs, git: g}
}

// Build classifies every entry in the hub config (and every dir registered
// in git's worktree list) into an Action. Pure read-only.
//
// pathspec restricts the plan to specific worktree paths or hop.json
// branch keys. An empty pathspec means all worktrees.
func (p *Planner) Build(hubPath string, pathspec []string) (*Plan, error) {
	hub, err := LoadHub(p.fs, hubPath)
	if err != nil {
		return nil, fmt.Errorf("load hub: %w", err)
	}

	porcelain, err := p.git.WorktreeListPorcelain(hubPath)
	if err != nil {
		return nil, fmt.Errorf("list worktrees: %w", err)
	}
	registered := parsePorcelainWorktrees(porcelain)

	plan := &Plan{HubPath: hubPath}

	hubKeys := sortedHubKeys(hub.Config.Branches)
	pathFilter := newPathFilter(pathspec, hubPath)
	for _, branchName := range hubKeys {
		branch := hub.Config.Branches[branchName]
		wtPath := absHubBranchPath(hubPath, branch.Path)
		if !pathFilter.matches(branchName, wtPath) {
			continue
		}
		action := p.classifyBranch(branchName, wtPath, registered)
		plan.Actions = append(plan.Actions, action)
	}

	// Worktrees git knows about but hop.json doesn't — orphan-in-git.
	for _, regPath := range registered {
		if isAbsRegisteredInHub(regPath, hub) {
			continue
		}
		// Skip the hub itself (some hubs are bare; git lists them too).
		if regPath == hubPath {
			continue
		}
		if !pathFilter.matches("", regPath) {
			continue
		}
		exists, _ := afero.DirExists(p.fs, regPath)
		if exists {
			// Registered in git, on disk, but absent from hop.json.
			plan.Actions = append(plan.Actions, Action{
				Kind:         ActionUpdateHopJSON,
				WorktreePath: regPath,
				Reason:       "registered in git but missing from hop.json",
			})
			continue
		}
		// Registered in git but directory gone.
		plan.Actions = append(plan.Actions, Action{
			Kind:         ActionUnregisterFromGit,
			WorktreePath: regPath,
			Reason:       "directory missing on disk",
		})
	}

	return plan, nil
}

// classifyBranch decides what (if anything) to do with one hub branch.
func (p *Planner) classifyBranch(name, wtPath string, registered []string) Action {
	exists, _ := afero.DirExists(p.fs, wtPath)
	registeredInGit := pathInRegistered(wtPath, registered)

	switch {
	case !exists && !registeredInGit:
		return Action{
			Kind:         ActionUpdateHopJSON,
			WorktreePath: wtPath,
			Reason:       fmt.Sprintf("hop.json references missing path for branch %q", name),
		}
	case !exists && registeredInGit:
		return Action{
			Kind:         ActionUnregisterFromGit,
			WorktreePath: wtPath,
			Reason:       "directory missing on disk but still registered in git",
		}
	case exists && !registeredInGit:
		return Action{
			Kind:         ActionRegisterWithGit,
			WorktreePath: wtPath,
			Reason:       "directory present but not registered in git",
		}
	default:
		// Both present. Check the .git pointer points at a real gitdir.
		if reason, ok := p.gitdirStale(wtPath); ok {
			return Action{
				Kind:         ActionRewriteGitdir,
				WorktreePath: wtPath,
				OldValue:     reason,
				Reason:       "stale .git pointer",
			}
		}
		return Action{Kind: ActionNoOp, WorktreePath: wtPath, Reason: "ok"}
	}
}

// gitdirStale checks whether the worktree's .git pointer file references
// a gitdir path that does not exist on disk. Returns (currentPointer, true)
// when stale, ("", false) when healthy or unreadable.
func (p *Planner) gitdirStale(wtPath string) (string, bool) {
	gitPointer := filepath.Join(wtPath, ".git")
	info, err := p.fs.Stat(gitPointer)
	if err != nil {
		return "", false
	}
	if info.IsDir() {
		return "", false
	}
	content, err := afero.ReadFile(p.fs, gitPointer)
	if err != nil {
		return "", false
	}
	line := strings.TrimSpace(string(content))
	const prefix = "gitdir:"
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	gitdirPath := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if gitdirPath == "" {
		return "", false
	}
	if _, err := p.fs.Stat(gitdirPath); err != nil {
		return gitdirPath, true
	}
	return "", false
}

// parsePorcelainWorktrees extracts absolute worktree paths from the
// `git worktree list --porcelain` output. Records are blank-line
// separated; each record begins with "worktree <abs-path>".
func parsePorcelainWorktrees(out string) []string {
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			paths = append(paths, strings.TrimSpace(strings.TrimPrefix(line, "worktree ")))
		}
	}
	return paths
}

func pathInRegistered(p string, registered []string) bool {
	for _, r := range registered {
		if r == p {
			return true
		}
	}
	return false
}

// absHubBranchPath resolves a HubBranch.Path field against the hub root.
// HubBranch.Path is stored as either a relative or absolute path; relative
// paths join under hubPath.
func absHubBranchPath(hubPath, raw string) string {
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw)
	}
	return filepath.Clean(filepath.Join(hubPath, raw))
}

func isAbsRegisteredInHub(absPath string, hub *Hub) bool {
	for _, b := range hub.Config.Branches {
		if absHubBranchPath(hub.Path, b.Path) == absPath {
			return true
		}
	}
	return false
}

func sortedHubKeys(m map[string]config.HubBranch) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// pathFilter implements the optional pathspec restriction.
type pathFilter struct {
	all  bool
	keys map[string]struct{}
	abs  map[string]struct{}
}

func newPathFilter(pathspec []string, hubPath string) *pathFilter {
	if len(pathspec) == 0 {
		return &pathFilter{all: true}
	}
	pf := &pathFilter{
		keys: make(map[string]struct{}, len(pathspec)),
		abs:  make(map[string]struct{}, len(pathspec)),
	}
	for _, raw := range pathspec {
		pf.keys[raw] = struct{}{}
		pf.abs[absHubBranchPath(hubPath, raw)] = struct{}{}
	}
	return pf
}

func (pf *pathFilter) matches(branchKey, absPath string) bool {
	if pf.all {
		return true
	}
	if branchKey != "" {
		if _, ok := pf.keys[branchKey]; ok {
			return true
		}
	}
	if _, ok := pf.abs[absPath]; ok {
		return true
	}
	return false
}
