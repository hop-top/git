package detector

import (
	"context"

	"github.com/spf13/afero"
)

type BranchTypeInfo struct {
	Type       string
	Name       string
	Prefix     string
	Parent     string
	StartPoint string
	Metadata   map[string]string
	Source     string
}

type Detector interface {
	Name() string
	Priority() int
	IsAvailable(repoPath string) bool
	Detect(branch string, repoPath string) (*BranchTypeInfo, error)
	OnAdd(ctx context.Context, info *BranchTypeInfo, worktreePath string, repoPath string) error
	OnRemove(ctx context.Context, info *BranchTypeInfo, worktreePath string, repoPath string) error
}

type GitInterface interface {
	GetConfig(repoPath, key string) (string, error)
	GetConfigRegex(repoPath, pattern string) (map[string]string, error)
	RunGitFlowStart(dir, branchType, name, base string) error
	RunGitFlowFinish(dir, branchType, name string) error
	RunInDir(dir string, cmd string, args ...string) (string, error)
}

type Manager struct {
	fs        afero.Fs
	git       GitInterface
	detectors []Detector
}

func NewManager(fs afero.Fs, git GitInterface) *Manager {
	return &Manager{
		fs:        fs,
		git:       git,
		detectors: make([]Detector, 0),
	}
}

func (m *Manager) Register(detector Detector) {
	m.detectors = append(m.detectors, detector)
	m.sortDetectors()
}

func (m *Manager) sortDetectors() {
	for i := 0; i < len(m.detectors)-1; i++ {
		for j := i + 1; j < len(m.detectors); j++ {
			if m.detectors[i].Priority() > m.detectors[j].Priority() {
				m.detectors[i], m.detectors[j] = m.detectors[j], m.detectors[i]
			}
		}
	}
}

func (m *Manager) DetectBranch(branch string, repoPath string) (*BranchTypeInfo, error) {
	for _, d := range m.detectors {
		if !d.IsAvailable(repoPath) {
			continue
		}
		info, err := d.Detect(branch, repoPath)
		if err != nil {
			return nil, err
		}
		if info != nil {
			return info, nil
		}
	}
	return nil, nil
}

// ExecuteAdd runs the add action of the detector that detected info, once
// the worktree at worktreePath exists. A nil info has none.
func (m *Manager) ExecuteAdd(ctx context.Context, info *BranchTypeInfo, repoPath string, worktreePath string) error {
	if info == nil {
		return nil
	}
	for _, d := range m.detectors {
		if d.Name() == info.Source {
			return d.OnAdd(ctx, info, worktreePath, repoPath)
		}
	}
	return nil
}

func (m *Manager) ExecutePreRemove(ctx context.Context, branch string, repoPath string, worktreePath string) (*BranchTypeInfo, error) {
	info, err := m.DetectBranch(branch, repoPath)
	if err != nil {
		return nil, err
	}

	if info == nil {
		return nil, nil
	}

	for _, d := range m.detectors {
		if d.Name() == info.Source {
			if err := d.OnRemove(ctx, info, worktreePath, repoPath); err != nil {
				return nil, err
			}
			break
		}
	}

	return info, nil
}

func (m *Manager) GetDetectorEnvVars(info *BranchTypeInfo) map[string]string {
	envVars := make(map[string]string)
	if info == nil {
		return envVars
	}

	envVars["GIT_HOP_BRANCH_TYPE"] = info.Type
	envVars["GIT_HOP_BRANCH_NAME"] = info.Name
	envVars["GIT_HOP_BRANCH_PREFIX"] = info.Prefix
	envVars["GIT_HOP_BRANCH_PARENT"] = info.Parent
	envVars["GIT_HOP_DETECTOR_SOURCE"] = info.Source

	if info.StartPoint != "" {
		envVars["GIT_HOP_BRANCH_START_POINT"] = info.StartPoint
	}

	return envVars
}
