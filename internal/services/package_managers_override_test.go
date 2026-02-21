package services_test

import (
	"testing"

	"hop.top/git/internal/config"
	"hop.top/git/internal/services"
	"github.com/spf13/afero"
)

func TestLoadPackageManagers_OverrideBuiltIn(t *testing.T) {
	tests := []struct {
		name                string
		customPM            config.PackageManagerConfig
		verifyPMName        string
		expectedDetectFiles []string
		expectedLockFiles   []string
		expectedDepsDir     string
		expectedInstallCmd  []string
	}{
		{
			name: "override npm with custom config",
			customPM: config.PackageManagerConfig{
				Name:        "npm",
				DetectFiles: []string{"custom-package.json"},
				LockFiles:   []string{"custom-lock.json"},
				DepsDir:     "custom_modules",
				InstallCmd:  []string{"echo", "custom-install"},
			},
			verifyPMName:        "npm",
			expectedDetectFiles: []string{"custom-package.json"},
			expectedLockFiles:   []string{"custom-lock.json"},
			expectedDepsDir:     "custom_modules",
			expectedInstallCmd:  []string{"echo", "custom-install"},
		},
		{
			name: "override go with different lock file priority",
			customPM: config.PackageManagerConfig{
				Name:        "go",
				DetectFiles: []string{"go.mod"},
				LockFiles:   []string{"go.work.sum", "go.sum"},
				DepsDir:     "vendor",
				InstallCmd:  []string{"sh", "-c", "go mod download"},
			},
			verifyPMName:        "go",
			expectedDetectFiles: []string{"go.mod"},
			expectedLockFiles:   []string{"go.work.sum", "go.sum"},
			expectedDepsDir:     "vendor",
			expectedInstallCmd:  []string{"sh", "-c", "go mod download"},
		},
		{
			name: "override pip with custom venv location",
			customPM: config.PackageManagerConfig{
				Name:        "pip",
				DetectFiles: []string{"requirements.txt"},
				LockFiles:   []string{"requirements.lock"},
				DepsDir:     ".venv",
				InstallCmd:  []string{"echo", "pip-install"},
			},
			verifyPMName:        "pip",
			expectedDetectFiles: []string{"requirements.txt"},
			expectedLockFiles:   []string{"requirements.lock"},
			expectedDepsDir:     ".venv",
			expectedInstallCmd:  []string{"echo", "pip-install"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			globalCfg := &config.GlobalConfig{
				PackageManagers: []config.PackageManagerConfig{tt.customPM},
			}

			pms, err := services.LoadPackageManagers(globalCfg)
			if err != nil {
				t.Fatalf("LoadPackageManagers() error = %v", err)
			}

			// Find the overridden PM
			var overriddenPM *services.PackageManager
			for _, pm := range pms {
				if pm.Name == tt.verifyPMName {
					overriddenPM = &pm
					break
				}
			}

			if overriddenPM == nil {
				t.Fatalf("PM %s not found in loaded PMs", tt.verifyPMName)
			}

			// Verify all fields match the custom config
			if len(overriddenPM.DetectFiles) != len(tt.expectedDetectFiles) {
				t.Errorf("DetectFiles length = %d, want %d", len(overriddenPM.DetectFiles), len(tt.expectedDetectFiles))
			}
			for i, file := range tt.expectedDetectFiles {
				if i >= len(overriddenPM.DetectFiles) || overriddenPM.DetectFiles[i] != file {
					t.Errorf("DetectFiles[%d] = %v, want %v", i, overriddenPM.DetectFiles[i], file)
				}
			}

			if len(overriddenPM.LockFiles) != len(tt.expectedLockFiles) {
				t.Errorf("LockFiles length = %d, want %d", len(overriddenPM.LockFiles), len(tt.expectedLockFiles))
			}
			for i, file := range tt.expectedLockFiles {
				if i >= len(overriddenPM.LockFiles) || overriddenPM.LockFiles[i] != file {
					t.Errorf("LockFiles[%d] = %v, want %v", i, overriddenPM.LockFiles[i], file)
				}
			}

			if overriddenPM.DepsDir != tt.expectedDepsDir {
				t.Errorf("DepsDir = %v, want %v", overriddenPM.DepsDir, tt.expectedDepsDir)
			}

			if len(overriddenPM.InstallCmd) != len(tt.expectedInstallCmd) {
				t.Errorf("InstallCmd length = %d, want %d", len(overriddenPM.InstallCmd), len(tt.expectedInstallCmd))
			}
			for i, cmd := range tt.expectedInstallCmd {
				if i >= len(overriddenPM.InstallCmd) || overriddenPM.InstallCmd[i] != cmd {
					t.Errorf("InstallCmd[%d] = %v, want %v", i, overriddenPM.InstallCmd[i], cmd)
				}
			}
		})
	}
}

func TestLoadPackageManagers_OverrideOnlyAffectsSpecificPM(t *testing.T) {
	// Override npm but ensure other built-ins remain unchanged
	globalCfg := &config.GlobalConfig{
		PackageManagers: []config.PackageManagerConfig{
			{
				Name:        "npm",
				DetectFiles: []string{"custom-package.json"},
				LockFiles:   []string{"custom-lock.json"},
				DepsDir:     "custom_modules",
				InstallCmd:  []string{"echo", "custom-install"},
			},
		},
	}

	pms, err := services.LoadPackageManagers(globalCfg)
	if err != nil {
		t.Fatalf("LoadPackageManagers() error = %v", err)
	}

	// Find npm (should be overridden)
	var npm *services.PackageManager
	for _, pm := range pms {
		if pm.Name == "npm" {
			npm = &pm
			break
		}
	}

	if npm == nil {
		t.Fatal("npm not found")
	}

	// Verify npm is overridden
	if npm.DepsDir != "custom_modules" {
		t.Errorf("npm.DepsDir = %v, want custom_modules", npm.DepsDir)
	}

	// Find yarn (should be unchanged)
	var yarn *services.PackageManager
	for _, pm := range pms {
		if pm.Name == "yarn" {
			yarn = &pm
			break
		}
	}

	if yarn == nil {
		t.Fatal("yarn not found")
	}

	// Verify yarn is NOT overridden (should have default values)
	if yarn.DepsDir != "node_modules" {
		t.Errorf("yarn.DepsDir = %v, want node_modules (default)", yarn.DepsDir)
	}
	if len(yarn.LockFiles) != 1 || yarn.LockFiles[0] != "yarn.lock" {
		t.Errorf("yarn.LockFiles = %v, want [yarn.lock] (default)", yarn.LockFiles)
	}
}

func TestLoadPackageManagers_MultipleOverrides(t *testing.T) {
	// Override multiple built-in PMs at once
	globalCfg := &config.GlobalConfig{
		PackageManagers: []config.PackageManagerConfig{
			{
				Name:        "npm",
				DetectFiles: []string{"custom-package.json"},
				LockFiles:   []string{"custom-lock.json"},
				DepsDir:     "custom_npm_modules",
				InstallCmd:  []string{"echo", "npm-install"},
			},
			{
				Name:        "go",
				DetectFiles: []string{"go.mod"},
				LockFiles:   []string{"go.work.sum"},
				DepsDir:     "custom_vendor",
				InstallCmd:  []string{"echo", "go-install"},
			},
			{
				Name:        "pip",
				DetectFiles: []string{"pyproject.toml"},
				LockFiles:   []string{"poetry.lock"},
				DepsDir:     ".venv",
				InstallCmd:  []string{"echo", "poetry-install"},
			},
		},
	}

	pms, err := services.LoadPackageManagers(globalCfg)
	if err != nil {
		t.Fatalf("LoadPackageManagers() error = %v", err)
	}

	// Build a map for easy lookup
	pmMap := make(map[string]services.PackageManager)
	for _, pm := range pms {
		pmMap[pm.Name] = pm
	}

	// Verify npm override
	npm, ok := pmMap["npm"]
	if !ok {
		t.Fatal("npm not found")
	}
	if npm.DepsDir != "custom_npm_modules" {
		t.Errorf("npm.DepsDir = %v, want custom_npm_modules", npm.DepsDir)
	}

	// Verify go override
	goPM, ok := pmMap["go"]
	if !ok {
		t.Fatal("go not found")
	}
	if goPM.DepsDir != "custom_vendor" {
		t.Errorf("go.DepsDir = %v, want custom_vendor", goPM.DepsDir)
	}

	// Verify pip override
	pip, ok := pmMap["pip"]
	if !ok {
		t.Fatal("pip not found")
	}
	if pip.DepsDir != ".venv" {
		t.Errorf("pip.DepsDir = %v, want .venv", pip.DepsDir)
	}

	// Verify other built-ins are unchanged
	yarn, ok := pmMap["yarn"]
	if !ok {
		t.Fatal("yarn not found")
	}
	if yarn.DepsDir != "node_modules" {
		t.Errorf("yarn.DepsDir = %v, want node_modules (default)", yarn.DepsDir)
	}
}

func TestLoadPackageManagers_OverrideAndDetection(t *testing.T) {
	// Test that overridden PM still works with detection
	globalCfg := &config.GlobalConfig{
		PackageManagers: []config.PackageManagerConfig{
			{
				Name:        "npm",
				DetectFiles: []string{"custom-package.json"},
				LockFiles:   []string{"custom-lock.json"},
				DepsDir:     "custom_modules",
				InstallCmd:  []string{"echo", "install"},
			},
		},
	}

	pms, err := services.LoadPackageManagers(globalCfg)
	if err != nil {
		t.Fatalf("LoadPackageManagers() error = %v", err)
	}

	// Create in-memory filesystem with custom files
	fs := afero.NewMemMapFs()
	worktreePath := "/test/worktree"
	if err := fs.MkdirAll(worktreePath, 0755); err != nil {
		t.Fatalf("failed to create worktree: %v", err)
	}

	// Create custom detect and lock files
	if err := afero.WriteFile(fs, worktreePath+"/custom-package.json", []byte(`{"name": "test"}`), 0644); err != nil {
		t.Fatalf("failed to create custom-package.json: %v", err)
	}
	if err := afero.WriteFile(fs, worktreePath+"/custom-lock.json", []byte(`{"lockfileVersion": 2}`), 0644); err != nil {
		t.Fatalf("failed to create custom-lock.json: %v", err)
	}

	// Detection should find the overridden npm
	detected, err := services.DetectPackageManagers(fs, worktreePath, pms)
	if err != nil {
		t.Fatalf("DetectPackageManagers() error = %v", err)
	}

	if len(detected) != 1 {
		t.Fatalf("DetectPackageManagers() detected %d PMs, want 1", len(detected))
	}

	if detected[0].Name != "npm" {
		t.Errorf("Detected PM name = %v, want npm", detected[0].Name)
	}

	// Verify it's the overridden version
	if detected[0].DepsDir != "custom_modules" {
		t.Errorf("Detected PM DepsDir = %v, want custom_modules", detected[0].DepsDir)
	}
}

func TestLoadPackageManagers_OverrideDoesNotDetectDefault(t *testing.T) {
	// Test that overriding a PM means it no longer detects with default files
	globalCfg := &config.GlobalConfig{
		PackageManagers: []config.PackageManagerConfig{
			{
				Name:        "npm",
				DetectFiles: []string{"custom-package.json"},
				LockFiles:   []string{"custom-lock.json"},
				DepsDir:     "custom_modules",
				InstallCmd:  []string{"echo", "install"},
			},
		},
	}

	pms, err := services.LoadPackageManagers(globalCfg)
	if err != nil {
		t.Fatalf("LoadPackageManagers() error = %v", err)
	}

	// Create in-memory filesystem with DEFAULT npm files
	fs := afero.NewMemMapFs()
	worktreePath := "/test/worktree"
	if err := fs.MkdirAll(worktreePath, 0755); err != nil {
		t.Fatalf("failed to create worktree: %v", err)
	}

	// Create standard npm files
	if err := afero.WriteFile(fs, worktreePath+"/package.json", []byte(`{"name": "test"}`), 0644); err != nil {
		t.Fatalf("failed to create package.json: %v", err)
	}
	if err := afero.WriteFile(fs, worktreePath+"/package-lock.json", []byte(`{"lockfileVersion": 2}`), 0644); err != nil {
		t.Fatalf("failed to create package-lock.json: %v", err)
	}

	// Detection should NOT find npm because it's looking for custom files
	detected, err := services.DetectPackageManagers(fs, worktreePath, pms)
	if err != nil {
		t.Fatalf("DetectPackageManagers() error = %v", err)
	}

	// Should not detect npm with default files
	for _, pm := range detected {
		if pm.Name == "npm" {
			t.Errorf("npm should not be detected with default files when overridden with custom detect files")
		}
	}
}

func TestLoadPackageManagers_OverridePlusCustom(t *testing.T) {
	// Test mixing overrides and new custom PMs
	globalCfg := &config.GlobalConfig{
		PackageManagers: []config.PackageManagerConfig{
			{
				// Override npm
				Name:        "npm",
				DetectFiles: []string{"custom-package.json"},
				LockFiles:   []string{"custom-lock.json"},
				DepsDir:     "custom_modules",
				InstallCmd:  []string{"echo", "npm-install"},
			},
			{
				// Add new custom PM
				Name:        "custom-pm",
				DetectFiles: []string{"custom.json"},
				LockFiles:   []string{"custom.lock"},
				DepsDir:     "custom_deps",
				InstallCmd:  []string{"echo", "custom-install"},
			},
		},
	}

	pms, err := services.LoadPackageManagers(globalCfg)
	if err != nil {
		t.Fatalf("LoadPackageManagers() error = %v", err)
	}

	// Build map
	pmMap := make(map[string]services.PackageManager)
	for _, pm := range pms {
		pmMap[pm.Name] = pm
	}

	// Verify npm is overridden
	npm, ok := pmMap["npm"]
	if !ok {
		t.Fatal("npm not found")
	}
	if npm.DepsDir != "custom_modules" {
		t.Errorf("npm.DepsDir = %v, want custom_modules", npm.DepsDir)
	}

	// Verify custom PM exists
	customPM, ok := pmMap["custom-pm"]
	if !ok {
		t.Fatal("custom-pm not found")
	}
	if customPM.DepsDir != "custom_deps" {
		t.Errorf("custom-pm.DepsDir = %v, want custom_deps", customPM.DepsDir)
	}

	// Verify other built-ins still exist
	if _, ok := pmMap["yarn"]; !ok {
		t.Error("yarn should still be present")
	}
	if _, ok := pmMap["go"]; !ok {
		t.Error("go should still be present")
	}

	// Total count should be 8 built-ins + 1 custom (npm is overridden, not added)
	if len(pms) != 9 {
		t.Errorf("LoadPackageManagers() returned %d PMs, want 9", len(pms))
	}
}

// Tests for hierarchical install command resolution

func TestResolveInstallCmd_NoOverrides(t *testing.T) {
	pm := services.PackageManager{
		Name:       "npm",
		InstallCmd: []string{"npm", "ci"},
	}

	hopspaceConfig := &config.HopspaceConfig{
		Branches: make(map[string]config.HopspaceBranch),
	}

	resolved := services.ResolveInstallCmd(pm, hopspaceConfig, "main")

	if len(resolved.InstallCmd) != 2 || resolved.InstallCmd[0] != "npm" || resolved.InstallCmd[1] != "ci" {
		t.Errorf("ResolveInstallCmd() = %v, want [npm ci]", resolved.InstallCmd)
	}
}

func TestResolveInstallCmd_RepoLevelOverride(t *testing.T) {
	pm := services.PackageManager{
		Name:       "npm",
		InstallCmd: []string{"npm", "ci"},
	}

	hopspaceConfig := &config.HopspaceConfig{
		PackageManagers: map[string]config.PackageManagerOverride{
			"npm": {
				InstallCmd: []string{"npm", "install", "--legacy-peer-deps"},
			},
		},
		Branches: map[string]config.HopspaceBranch{
			"main": {
				Exists: true,
				Path:   "/path/to/main",
			},
		},
	}

	resolved := services.ResolveInstallCmd(pm, hopspaceConfig, "main")

	expected := []string{"npm", "install", "--legacy-peer-deps"}
	if len(resolved.InstallCmd) != len(expected) {
		t.Errorf("ResolveInstallCmd() length = %d, want %d", len(resolved.InstallCmd), len(expected))
	}
	for i, cmd := range expected {
		if resolved.InstallCmd[i] != cmd {
			t.Errorf("ResolveInstallCmd()[%d] = %v, want %v", i, resolved.InstallCmd[i], cmd)
		}
	}
}

func TestResolveInstallCmd_BranchLevelOverride(t *testing.T) {
	pm := services.PackageManager{
		Name:       "npm",
		InstallCmd: []string{"npm", "ci"},
	}

	hopspaceConfig := &config.HopspaceConfig{
		PackageManagers: map[string]config.PackageManagerOverride{
			"npm": {
				InstallCmd: []string{"npm", "install", "--legacy-peer-deps"},
			},
		},
		Branches: map[string]config.HopspaceBranch{
			"main": {
				Exists: true,
				Path:   "/path/to/main",
			},
			"feature-x": {
				Exists: true,
				Path:   "/path/to/feature-x",
				PackageManagers: map[string]config.PackageManagerOverride{
					"npm": {
						InstallCmd: []string{"npm", "install", "--force"},
					},
				},
			},
		},
	}

	// main branch uses repo-level override
	resolvedMain := services.ResolveInstallCmd(pm, hopspaceConfig, "main")
	expectedMain := []string{"npm", "install", "--legacy-peer-deps"}
	for i, cmd := range expectedMain {
		if resolvedMain.InstallCmd[i] != cmd {
			t.Errorf("main branch: InstallCmd[%d] = %v, want %v", i, resolvedMain.InstallCmd[i], cmd)
		}
	}

	// feature-x branch uses branch-level override
	resolvedFeature := services.ResolveInstallCmd(pm, hopspaceConfig, "feature-x")
	expectedFeature := []string{"npm", "install", "--force"}
	for i, cmd := range expectedFeature {
		if resolvedFeature.InstallCmd[i] != cmd {
			t.Errorf("feature-x branch: InstallCmd[%d] = %v, want %v", i, resolvedFeature.InstallCmd[i], cmd)
		}
	}
}

func TestResolveInstallCmd_NilConfig(t *testing.T) {
	pm := services.PackageManager{
		Name:       "npm",
		InstallCmd: []string{"npm", "ci"},
	}

	resolved := services.ResolveInstallCmd(pm, nil, "main")

	if len(resolved.InstallCmd) != 2 || resolved.InstallCmd[0] != "npm" || resolved.InstallCmd[1] != "ci" {
		t.Errorf("ResolveInstallCmd() with nil config = %v, want [npm ci]", resolved.InstallCmd)
	}
}

func TestResolveInstallCmd_EmptyOverride(t *testing.T) {
	pm := services.PackageManager{
		Name:       "npm",
		InstallCmd: []string{"npm", "ci"},
	}

	hopspaceConfig := &config.HopspaceConfig{
		PackageManagers: map[string]config.PackageManagerOverride{
			"npm": {
				InstallCmd: []string{}, // Empty override should be ignored
			},
		},
		Branches: make(map[string]config.HopspaceBranch),
	}

	resolved := services.ResolveInstallCmd(pm, hopspaceConfig, "main")

	if len(resolved.InstallCmd) != 2 || resolved.InstallCmd[0] != "npm" || resolved.InstallCmd[1] != "ci" {
		t.Errorf("ResolveInstallCmd() with empty override = %v, want [npm ci]", resolved.InstallCmd)
	}
}

func TestResolveInstallCmd_PreservesOtherFields(t *testing.T) {
	pm := services.PackageManager{
		Name:        "npm",
		DetectFiles: []string{"package.json"},
		LockFiles:   []string{"package-lock.json"},
		DepsDir:     "node_modules",
		InstallCmd:  []string{"npm", "ci"},
	}

	hopspaceConfig := &config.HopspaceConfig{
		PackageManagers: map[string]config.PackageManagerOverride{
			"npm": {
				InstallCmd: []string{"npm", "install", "--legacy-peer-deps"},
			},
		},
		Branches: make(map[string]config.HopspaceBranch),
	}

	resolved := services.ResolveInstallCmd(pm, hopspaceConfig, "main")

	// Only InstallCmd should change
	if resolved.Name != "npm" {
		t.Errorf("Name = %v, want npm", resolved.Name)
	}
	if len(resolved.DetectFiles) != 1 || resolved.DetectFiles[0] != "package.json" {
		t.Errorf("DetectFiles = %v, want [package.json]", resolved.DetectFiles)
	}
	if len(resolved.LockFiles) != 1 || resolved.LockFiles[0] != "package-lock.json" {
		t.Errorf("LockFiles = %v, want [package-lock.json]", resolved.LockFiles)
	}
	if resolved.DepsDir != "node_modules" {
		t.Errorf("DepsDir = %v, want node_modules", resolved.DepsDir)
	}
	if len(resolved.InstallCmd) != 3 || resolved.InstallCmd[0] != "npm" {
		t.Errorf("InstallCmd = %v, want [npm install --legacy-peer-deps]", resolved.InstallCmd)
	}
}

func TestApplyOverride_NilOverride(t *testing.T) {
	pm := services.PackageManager{
		Name:       "npm",
		InstallCmd: []string{"npm", "ci"},
	}

	result := pm.ApplyOverride(nil)

	// Should return same PM instance
	if result != &pm {
		t.Error("ApplyOverride(nil) should return same PM instance")
	}
}

func TestApplyOverride_EmptyInstallCmd(t *testing.T) {
	pm := services.PackageManager{
		Name:       "npm",
		InstallCmd: []string{"npm", "ci"},
	}

	override := &config.PackageManagerOverride{
		InstallCmd: []string{},
	}

	result := pm.ApplyOverride(override)

	// Should return same PM instance when override is empty
	if result != &pm {
		t.Error("ApplyOverride with empty InstallCmd should return same PM instance")
	}
}

func TestApplyOverride_WithInstallCmd(t *testing.T) {
	pm := services.PackageManager{
		Name:       "npm",
		InstallCmd: []string{"npm", "ci"},
	}

	override := &config.PackageManagerOverride{
		InstallCmd: []string{"npm", "install", "--legacy-peer-deps"},
	}

	result := pm.ApplyOverride(override)

	// Should return new PM with overridden command
	if result == &pm {
		t.Error("ApplyOverride with InstallCmd should return new PM instance")
	}
	if len(result.InstallCmd) != 3 || result.InstallCmd[0] != "npm" || result.InstallCmd[2] != "--legacy-peer-deps" {
		t.Errorf("ApplyOverride InstallCmd = %v, want [npm install --legacy-peer-deps]", result.InstallCmd)
	}
	// Original should be unchanged
	if len(pm.InstallCmd) != 2 || pm.InstallCmd[0] != "npm" || pm.InstallCmd[1] != "ci" {
		t.Errorf("Original PM InstallCmd changed: %v, want [npm ci]", pm.InstallCmd)
	}
}
