package detector

import (
	"errors"
	"testing"

	"github.com/spf13/afero"
)

type mockGitForDetector struct {
	configValues    map[string]string
	configRegexVals map[string]map[string]string
	flowStartErr    error
	flowFinishErr   error
}

func (m *mockGitForDetector) GetConfig(repoPath, key string) (string, error) {
	if m.configValues == nil {
		return "", errors.New("not found")
	}
	val, ok := m.configValues[key]
	if !ok {
		return "", errors.New("not found")
	}
	return val, nil
}

func (m *mockGitForDetector) GetConfigRegex(repoPath, pattern string) (map[string]string, error) {
	if m.configRegexVals == nil {
		return nil, nil
	}
	return m.configRegexVals[pattern], nil
}

func (m *mockGitForDetector) RunGitFlowStart(repoPath, branchType, name string) error {
	return m.flowStartErr
}

func (m *mockGitForDetector) RunGitFlowFinish(repoPath, branchType, name string) error {
	return m.flowFinishErr
}

func TestGitFlowNextDetector_Name(t *testing.T) {
	d := NewGitFlowNextDetector(nil)
	if d.Name() != "gitflow-next" {
		t.Errorf("Expected name 'gitflow-next', got '%s'", d.Name())
	}
}

func TestGitFlowNextDetector_Priority(t *testing.T) {
	d := NewGitFlowNextDetector(nil)
	if d.Priority() != 10 {
		t.Errorf("Expected priority 10, got %d", d.Priority())
	}
}

func TestGitFlowNextDetector_IsAvailable(t *testing.T) {
	tests := []struct {
		name        string
		configValue string
		expected    bool
	}{
		{"initialized true", "true", true},
		{"initialized false", "false", false},
		{"not set", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGit := &mockGitForDetector{
				configValues: map[string]string{
					"gitflow.initialized": tt.configValue,
				},
			}
			d := NewGitFlowNextDetector(mockGit)
			result := d.IsAvailable("/repo")
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestGitFlowNextDetector_Detect(t *testing.T) {
	tests := []struct {
		name         string
		branch       string
		prefixConfig map[string]string
		parentConfig map[string]string
		expectedType string
		expectedName string
		expectedNil  bool
	}{
		{
			name:   "feature branch",
			branch: "feature/my-feature",
			prefixConfig: map[string]string{
				"gitflow.branch.feature.prefix": "feature/",
			},
			parentConfig: map[string]string{
				"gitflow.branch.feature.parent": "develop",
			},
			expectedType: "feature",
			expectedName: "my-feature",
		},
		{
			name:   "release branch",
			branch: "release/v1.0.0",
			prefixConfig: map[string]string{
				"gitflow.branch.release.prefix": "release/",
			},
			parentConfig: map[string]string{
				"gitflow.branch.release.parent": "main",
			},
			expectedType: "release",
			expectedName: "v1.0.0",
		},
		{
			name:   "custom bugfix branch",
			branch: "bugfix/fix-login",
			prefixConfig: map[string]string{
				"gitflow.branch.bugfix.prefix": "bugfix/",
			},
			parentConfig: map[string]string{
				"gitflow.branch.bugfix.parent": "develop",
			},
			expectedType: "bugfix",
			expectedName: "fix-login",
		},
		{
			name:   "no matching prefix",
			branch: "random-branch",
			prefixConfig: map[string]string{
				"gitflow.branch.feature.prefix": "feature/",
			},
			expectedNil: true,
		},
		{
			name:         "empty config",
			branch:       "feature/test",
			prefixConfig: nil,
			expectedNil:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockGit := &mockGitForDetector{
				configRegexVals: map[string]map[string]string{
					"^gitflow\\.branch\\..*\\.prefix$": tt.prefixConfig,
				},
				configValues: tt.parentConfig,
			}
			d := NewGitFlowNextDetector(mockGit)

			info, err := d.Detect(tt.branch, "/repo")
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if tt.expectedNil {
				if info != nil {
					t.Errorf("Expected nil, got %+v", info)
				}
				return
			}

			if info == nil {
				t.Fatalf("Expected info, got nil")
			}

			if info.Type != tt.expectedType {
				t.Errorf("Expected type '%s', got '%s'", tt.expectedType, info.Type)
			}

			if info.Name != tt.expectedName {
				t.Errorf("Expected name '%s', got '%s'", tt.expectedName, info.Name)
			}

			if info.Source != "gitflow-next" {
				t.Errorf("Expected source 'gitflow-next', got '%s'", info.Source)
			}
		})
	}
}

func TestGenericDetector_Name(t *testing.T) {
	d := NewGenericDetector(nil)
	if d.Name() != "generic" {
		t.Errorf("Expected name 'generic', got '%s'", d.Name())
	}
}

func TestGenericDetector_Priority(t *testing.T) {
	d := NewGenericDetector(nil)
	if d.Priority() != 100 {
		t.Errorf("Expected priority 100, got %d", d.Priority())
	}
}

func TestGenericDetector_Detect(t *testing.T) {
	config := map[string]BranchTypeConfig{
		"feature": {
			Prefix:     "feature/",
			Parent:     "develop",
			StartPoint: "develop",
		},
		"bugfix": {
			Prefix: "bugfix/",
			Parent: "develop",
		},
		"release": {
			Prefix:     "release/",
			Parent:     "main",
			StartPoint: "develop",
		},
	}

	tests := []struct {
		name         string
		branch       string
		expectedType string
		expectedName string
		expectedNil  bool
	}{
		{"feature branch", "feature/my-feature", "feature", "my-feature", false},
		{"bugfix branch", "bugfix/fix-bug", "bugfix", "fix-bug", false},
		{"release branch", "release/v1.0.0", "release", "v1.0.0", false},
		{"no match", "random-branch", "", "", true},
		{"partial match", "featuretest", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewGenericDetector(config)
			info, err := d.Detect(tt.branch, "/repo")
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if tt.expectedNil {
				if info != nil {
					t.Errorf("Expected nil, got %+v", info)
				}
				return
			}

			if info == nil {
				t.Fatalf("Expected info, got nil")
			}

			if info.Type != tt.expectedType {
				t.Errorf("Expected type '%s', got '%s'", tt.expectedType, info.Type)
			}

			if info.Name != tt.expectedName {
				t.Errorf("Expected name '%s', got '%s'", tt.expectedName, info.Name)
			}

			if info.Source != "generic" {
				t.Errorf("Expected source 'generic', got '%s'", info.Source)
			}
		})
	}
}

func TestGenericDetector_IsAvailable(t *testing.T) {
	d := NewGenericDetector(nil)
	if d.IsAvailable("/repo") {
		t.Error("Expected IsAvailable to return false with nil config")
	}

	d = NewGenericDetector(map[string]BranchTypeConfig{
		"feature": {Prefix: "feature/"},
	})
	if !d.IsAvailable("/repo") {
		t.Error("Expected IsAvailable to return true with config")
	}
}

func TestDefaultGenericConfig(t *testing.T) {
	config := DefaultGenericConfig()

	expectedTypes := []string{"feature", "release", "hotfix", "support", "bugfix"}
	for _, expectedType := range expectedTypes {
		if _, ok := config[expectedType]; !ok {
			t.Errorf("Expected config to contain '%s' branch type", expectedType)
		}
	}

	if config["feature"].Prefix != "feature/" {
		t.Errorf("Expected feature prefix 'feature/', got '%s'", config["feature"].Prefix)
	}

	if config["feature"].Parent != "develop" {
		t.Errorf("Expected feature parent 'develop', got '%s'", config["feature"].Parent)
	}
}

func TestManager_Register(t *testing.T) {
	fs := afero.NewMemMapFs()
	m := NewManager(fs, nil)

	d1 := NewGenericDetector(nil)
	d2 := NewGitFlowNextDetector(nil)

	m.Register(d1)
	m.Register(d2)

	if len(m.detectors) != 2 {
		t.Errorf("Expected 2 detectors, got %d", len(m.detectors))
	}

	if m.detectors[0].Priority() != 10 {
		t.Errorf("Expected first detector priority 10, got %d", m.detectors[0].Priority())
	}

	if m.detectors[1].Priority() != 100 {
		t.Errorf("Expected second detector priority 100, got %d", m.detectors[1].Priority())
	}
}

func TestManager_GetDetectorEnvVars(t *testing.T) {
	m := NewManager(nil, nil)

	t.Run("nil info", func(t *testing.T) {
		env := m.GetDetectorEnvVars(nil)
		if len(env) != 0 {
			t.Errorf("Expected empty env vars, got %d", len(env))
		}
	})

	t.Run("with info", func(t *testing.T) {
		info := &BranchTypeInfo{
			Type:       "feature",
			Name:       "my-feature",
			Prefix:     "feature/",
			Parent:     "develop",
			StartPoint: "develop",
			Source:     "gitflow-next",
		}
		env := m.GetDetectorEnvVars(info)

		expectedVars := map[string]string{
			"GIT_HOP_BRANCH_TYPE":        "feature",
			"GIT_HOP_BRANCH_NAME":        "my-feature",
			"GIT_HOP_BRANCH_PREFIX":      "feature/",
			"GIT_HOP_BRANCH_PARENT":      "develop",
			"GIT_HOP_DETECTOR_SOURCE":    "gitflow-next",
			"GIT_HOP_BRANCH_START_POINT": "develop",
		}

		for key, expectedVal := range expectedVars {
			if env[key] != expectedVal {
				t.Errorf("Expected %s='%s', got '%s'", key, expectedVal, env[key])
			}
		}
	})
}
