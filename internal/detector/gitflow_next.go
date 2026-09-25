package detector

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// GitFlowNextDetector maps branches onto git-flow-next branch types by
// reading gitflow.* config. Detection is read-only. Its add/remove actions
// (`git flow <type> start|finish`) change the repo, and finish merges into
// the parent branch, so they are off unless WithGitFlowActions turns them
// on.
//
// git-flow-next needs a work tree to run in, and a git-hop hub is a bare
// repository, so both actions run in the branch's own worktree (see OnAdd
// and OnRemove), never in the hub.
type GitFlowNextDetector struct {
	git       GitInterface
	actions   bool
	startBase string
	skipped   func(info *BranchTypeInfo, action string)
}

// GitFlowOption configures a GitFlowNextDetector.
type GitFlowOption func(*GitFlowNextDetector)

// WithGitFlowActions lets OnAdd/OnRemove run `git flow <type> start` and
// `git flow <type> finish`.
func WithGitFlowActions(enabled bool) GitFlowOption {
	return func(d *GitFlowNextDetector) { d.actions = enabled }
}

// WithStartBase makes OnAdd pass base to `git flow <type> start` as its
// [base]: the branch starts there, and git-flow records it as
// gitflow.branch.<branch>.base. Finish still merges into the type's
// parent. Empty leaves the start point to the branch type.
func WithStartBase(base string) GitFlowOption {
	return func(d *GitFlowNextDetector) { d.startBase = base }
}

// WithSkippedAction registers fn to be told about each git-flow action
// (start or finish) not run because actions are off.
func WithSkippedAction(fn func(info *BranchTypeInfo, action string)) GitFlowOption {
	return func(d *GitFlowNextDetector) { d.skipped = fn }
}

func NewGitFlowNextDetector(git GitInterface, opts ...GitFlowOption) *GitFlowNextDetector {
	d := &GitFlowNextDetector{git: git}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// ActionsEnabled reports whether OnAdd/OnRemove run git-flow commands.
func (d *GitFlowNextDetector) ActionsEnabled() bool {
	return d.actions
}

// StartsBranch reports whether OnAdd creates the branch info describes,
// with `git flow <type> start`: info is one of this detector's and actions
// are on. The caller then creates the worktree detached, without the
// branch, for OnAdd to start it in.
func (d *GitFlowNextDetector) StartsBranch(info *BranchTypeInfo) bool {
	return d.actions && info != nil && info.Source == d.Name()
}

func (d *GitFlowNextDetector) Name() string {
	return "gitflow-next"
}

func (d *GitFlowNextDetector) Priority() int {
	return 10
}

func (d *GitFlowNextDetector) IsAvailable(repoPath string) bool {
	initialized, err := d.git.GetConfig(repoPath, "gitflow.initialized")
	if err != nil {
		return false
	}
	return initialized == "true"
}

func (d *GitFlowNextDetector) Detect(branch string, repoPath string) (*BranchTypeInfo, error) {
	configs, err := d.git.GetConfigRegex(repoPath, "^gitflow\\.branch\\..*\\.prefix$")
	if err != nil {
		return nil, fmt.Errorf("failed to get git-flow config: %w", err)
	}

	prefixToType := make(map[string]string)
	for key, prefix := range configs {
		parts := strings.Split(key, ".")
		if len(parts) >= 3 {
			branchType := parts[2]
			prefixToType[prefix] = branchType
		}
	}

	var matches []struct {
		prefix     string
		branchType string
		len        int
	}
	for prefix, branchType := range prefixToType {
		if strings.HasPrefix(branch, prefix) {
			matches = append(matches, struct {
				prefix     string
				branchType string
				len        int
			}{prefix: prefix, branchType: branchType, len: len(prefix)})
		}
	}

	if len(matches) == 0 {
		return nil, nil
	}

	longest := matches[0]
	for _, m := range matches[1:] {
		if m.len > longest.len {
			longest = m
		}
	}

	name := strings.TrimPrefix(branch, longest.prefix)

	parent := d.getConfig(repoPath, fmt.Sprintf("gitflow.branch.%s.parent", longest.branchType))
	startPoint := d.getConfig(repoPath, fmt.Sprintf("gitflow.branch.%s.startPoint", longest.branchType))
	if startPoint == "" {
		startPoint = parent
	}

	branchType := d.getConfig(repoPath, fmt.Sprintf("gitflow.branch.%s.type", longest.branchType))

	return &BranchTypeInfo{
		Type:       longest.branchType,
		Name:       name,
		Prefix:     longest.prefix,
		Parent:     parent,
		StartPoint: startPoint,
		Source:     d.Name(),
		Metadata: map[string]string{
			"branchType": branchType,
		},
	}, nil
}

func (d *GitFlowNextDetector) getConfig(repoPath, key string) string {
	val, err := d.git.GetConfig(repoPath, key)
	if err != nil {
		return ""
	}
	return val
}

// OnAdd runs `git flow <type> start <name> [<base>] --no-worktree` in
// worktreePath, the worktree created for the branch on a detached HEAD.
// git-flow creates the branch, records its base and checks it out there,
// so the branch is created once, by git-flow, and no other worktree
// changes branch. --no-worktree keeps git-flow from making a worktree of
// its own for a type configured with one: git-hop owns the worktree.
func (d *GitFlowNextDetector) OnAdd(ctx context.Context, info *BranchTypeInfo, worktreePath string, repoPath string) error {
	if info == nil || d.skip(info, "start") {
		return nil
	}

	if err := d.git.RunGitFlowStart(worktreePath, info.Type, info.Name, d.startBase); err != nil {
		return fmt.Errorf("git flow %s start %s failed: %w", info.Type, info.Name, err)
	}

	return nil
}

// OnRemove runs `git flow <type> finish <name>` before the branch's
// worktree at worktreePath is removed. See finishDir for where it runs.
func (d *GitFlowNextDetector) OnRemove(ctx context.Context, info *BranchTypeInfo, worktreePath string, repoPath string) error {
	if info == nil || d.skip(info, "finish") {
		return nil
	}

	dir, detached, err := d.finishDir(info, worktreePath, repoPath)
	if err != nil {
		return fmt.Errorf("git flow %s finish %s: %w", info.Type, info.Name, err)
	}
	if err := d.git.RunGitFlowFinish(dir, info.Type, info.Name); err != nil {
		if detached {
			d.reattach(dir, info.Prefix+info.Name)
		}
		return fmt.Errorf("git flow %s finish %s failed: %w", info.Type, info.Name, err)
	}

	return nil
}

// finishDir returns the worktree to run finish in, readying it first.
//
// git-flow-next 2.1 finishes into the target, the type's parent (not the
// base start recorded), in whichever worktree has it checked out;
// run from the branch's own worktree, it then detaches that worktree and
// deletes the branch. With the target checked out nowhere it falls back to
// the repository's main work tree, which a bare hub lacks, and from any
// other worktree it would check the target out there, taking that worktree
// off its branch. So the branch's worktree is where finish runs, and when
// the target is checked out nowhere it is detached first: git-flow then
// checks the target out in it, and the only worktree that changes is the
// one about to be removed; detached reports that it was. A branch whose
// worktree is gone finishes in the target's worktree.
func (d *GitFlowNextDetector) finishDir(info *BranchTypeInfo, worktreePath, repoPath string) (dir string, detached bool, err error) {
	target := info.Parent
	targetDir := d.checkedOutAt(repoPath, target)

	if st, err := os.Stat(worktreePath); err != nil || !st.IsDir() {
		if targetDir == "" {
			return "", false, fmt.Errorf("worktree %s is missing and '%s' is not checked out in any worktree", worktreePath, target)
		}
		return targetDir, false, nil
	}
	if targetDir == "" && target != "" {
		if _, err := d.git.RunInDir(worktreePath, "git", "checkout", "--quiet", "--detach"); err != nil {
			return "", false, fmt.Errorf("failed to detach %s: %w", worktreePath, err)
		}
		return worktreePath, true, nil
	}
	return worktreePath, false, nil
}

// reattach checks branch out again in dir after a failed finish, but only
// if dir is still detached: a finish that got as far as checking out its
// target (a merge conflict, say) is left for the user to resolve there.
func (d *GitFlowNextDetector) reattach(dir, branch string) {
	cur, err := d.git.RunInDir(dir, "git", "branch", "--show-current")
	if err != nil || strings.TrimSpace(cur) != "" {
		return
	}
	_, _ = d.git.RunInDir(dir, "git", "checkout", "--quiet", branch)
}

// checkedOutAt returns the worktree that has branch checked out, or "".
func (d *GitFlowNextDetector) checkedOutAt(repoPath, branch string) string {
	if branch == "" {
		return ""
	}
	out, err := d.git.RunInDir(repoPath, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return ""
	}
	var dir string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			dir = strings.TrimPrefix(line, "worktree ")
		case line == "branch refs/heads/"+branch:
			return dir
		}
	}
	return ""
}

// skip reports whether action must not run, telling the skip listener.
func (d *GitFlowNextDetector) skip(info *BranchTypeInfo, action string) bool {
	if d.actions {
		return false
	}
	if d.skipped != nil {
		d.skipped(info, action)
	}
	return true
}
