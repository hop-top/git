package detector

import (
	"context"
)

type BranchTypeConfig struct {
	Prefix     string
	Parent     string
	StartPoint string
}

type GenericDetector struct {
	config map[string]BranchTypeConfig
}

func NewGenericDetector(config map[string]BranchTypeConfig) *GenericDetector {
	return &GenericDetector{config: config}
}

func (d *GenericDetector) Name() string {
	return "generic"
}

func (d *GenericDetector) Priority() int {
	return 100
}

func (d *GenericDetector) IsAvailable(repoPath string) bool {
	return len(d.config) > 0
}

func (d *GenericDetector) Detect(branch string, repoPath string) (*BranchTypeInfo, error) {
	var matches []struct {
		branchType string
		prefix     string
		config     BranchTypeConfig
		len        int
	}

	for branchType, cfg := range d.config {
		if len(cfg.Prefix) > 0 && len(branch) >= len(cfg.Prefix) && branch[:len(cfg.Prefix)] == cfg.Prefix {
			matches = append(matches, struct {
				branchType string
				prefix     string
				config     BranchTypeConfig
				len        int
			}{branchType: branchType, prefix: cfg.Prefix, config: cfg, len: len(cfg.Prefix)})
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

	name := branch[len(longest.prefix):]
	startPoint := longest.config.StartPoint
	if startPoint == "" {
		startPoint = longest.config.Parent
	}

	return &BranchTypeInfo{
		Type:       longest.branchType,
		Name:       name,
		Prefix:     longest.prefix,
		Parent:     longest.config.Parent,
		StartPoint: startPoint,
		Source:     d.Name(),
	}, nil
}

func (d *GenericDetector) OnAdd(ctx context.Context, info *BranchTypeInfo, worktreePath string, repoPath string) error {
	return nil
}

func (d *GenericDetector) OnRemove(ctx context.Context, info *BranchTypeInfo, worktreePath string, repoPath string) error {
	return nil
}

func DefaultGenericConfig() map[string]BranchTypeConfig {
	return map[string]BranchTypeConfig{
		"feature": {
			Prefix:     "feature/",
			Parent:     "develop",
			StartPoint: "develop",
		},
		"release": {
			Prefix:     "release/",
			Parent:     "main",
			StartPoint: "develop",
		},
		"hotfix": {
			Prefix:     "hotfix/",
			Parent:     "main",
			StartPoint: "main",
		},
		"support": {
			Prefix:     "support/",
			Parent:     "main",
			StartPoint: "main",
		},
		"bugfix": {
			Prefix:     "bugfix/",
			Parent:     "develop",
			StartPoint: "develop",
		},
	}
}
