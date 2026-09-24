package detector

import (
	"context"
	"fmt"
	"strings"
)

// GitFlowNextDetector maps branches onto git-flow-next branch types by
// reading gitflow.* config. Detection is read-only. Its add/remove actions
// (`git flow <type> start|finish`) change the repo, and finish merges into
// the parent branch, so they are off unless WithGitFlowActions turns them
// on.
type GitFlowNextDetector struct {
	git     GitInterface
	actions bool
	skipped func(info *BranchTypeInfo, action string)
}

// GitFlowOption configures a GitFlowNextDetector.
type GitFlowOption func(*GitFlowNextDetector)

// WithGitFlowActions lets OnAdd/OnRemove run `git flow <type> start` and
// `git flow <type> finish`.
func WithGitFlowActions(enabled bool) GitFlowOption {
	return func(d *GitFlowNextDetector) { d.actions = enabled }
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

func (d *GitFlowNextDetector) OnAdd(ctx context.Context, info *BranchTypeInfo, worktreePath string, repoPath string) error {
	if info == nil || d.skip(info, "start") {
		return nil
	}

	if err := d.git.RunGitFlowStart(repoPath, info.Type, info.Name); err != nil {
		return fmt.Errorf("git flow %s start %s failed: %w", info.Type, info.Name, err)
	}

	return nil
}

func (d *GitFlowNextDetector) OnRemove(ctx context.Context, info *BranchTypeInfo, worktreePath string, repoPath string) error {
	if info == nil || d.skip(info, "finish") {
		return nil
	}

	if err := d.git.RunGitFlowFinish(repoPath, info.Type, info.Name); err != nil {
		return fmt.Errorf("git flow %s finish %s failed: %w", info.Type, info.Name, err)
	}

	return nil
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
