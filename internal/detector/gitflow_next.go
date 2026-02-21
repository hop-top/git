package detector

import (
	"context"
	"fmt"
	"strings"
)

type GitFlowNextDetector struct {
	git GitInterface
}

func NewGitFlowNextDetector(git GitInterface) *GitFlowNextDetector {
	return &GitFlowNextDetector{git: git}
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
	if info == nil {
		return nil
	}

	if err := d.git.RunGitFlowStart(repoPath, info.Type, info.Name); err != nil {
		return fmt.Errorf("git flow %s start %s failed: %w", info.Type, info.Name, err)
	}

	return nil
}

func (d *GitFlowNextDetector) OnRemove(ctx context.Context, info *BranchTypeInfo, worktreePath string, repoPath string) error {
	if info == nil {
		return nil
	}

	if err := d.git.RunGitFlowFinish(repoPath, info.Type, info.Name); err != nil {
		return fmt.Errorf("git flow %s finish %s failed: %w", info.Type, info.Name, err)
	}

	return nil
}
