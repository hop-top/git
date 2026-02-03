package hop_test

import (
	"testing"

	"github.com/jadb/git-hop/internal/config"
	"github.com/jadb/git-hop/internal/services"
)

func TestLoadPackageManagers_CustomConfig(t *testing.T) {
	tests := []struct {
		name        string
		globalCfg   *config.GlobalConfig
		expectedPMs map[string]bool
		expectError bool
	}{
		{
			name: "loads custom PM in addition to built-ins",
			globalCfg: &config.GlobalConfig{
				PackageManagers: []config.PackageManagerConfig{
					{
						Name:        "custom-pm",
						DetectFiles: []string{"custom.json"},
						LockFiles:   []string{"custom.lock"},
						DepsDir:     "custom_modules",
						InstallCmd:  []string{"echo", "install"},
					},
				},
			},
			expectedPMs: map[string]bool{
				"npm":       true,
				"pnpm":      true,
				"yarn":      true,
				"go":        true,
				"pip":       true,
				"cargo":     true,
				"composer":  true,
				"bundler":   true,
				"custom-pm": true,
			},
			expectError: false,
		},
		{
			name: "loads multiple custom PMs",
			globalCfg: &config.GlobalConfig{
				PackageManagers: []config.PackageManagerConfig{
					{
						Name:        "custom-pm-1",
						DetectFiles: []string{"custom1.json"},
						LockFiles:   []string{"custom1.lock"},
						DepsDir:     "custom1_modules",
						InstallCmd:  []string{"echo", "install1"},
					},
					{
						Name:        "custom-pm-2",
						DetectFiles: []string{"custom2.json"},
						LockFiles:   []string{"custom2.lock"},
						DepsDir:     "custom2_modules",
						InstallCmd:  []string{"echo", "install2"},
					},
				},
			},
			expectedPMs: map[string]bool{
				"npm":         true,
				"pnpm":        true,
				"yarn":        true,
				"go":          true,
				"pip":         true,
				"cargo":       true,
				"composer":    true,
				"bundler":     true,
				"custom-pm-1": true,
				"custom-pm-2": true,
			},
			expectError: false,
		},
		{
			name: "empty PackageManagers array returns only built-ins",
			globalCfg: &config.GlobalConfig{
				PackageManagers: []config.PackageManagerConfig{},
			},
			expectedPMs: map[string]bool{
				"npm":      true,
				"pnpm":     true,
				"yarn":     true,
				"go":       true,
				"pip":      true,
				"cargo":    true,
				"composer": true,
				"bundler":  true,
			},
			expectError: false,
		},
		{
			name: "invalid custom PM - missing name",
			globalCfg: &config.GlobalConfig{
				PackageManagers: []config.PackageManagerConfig{
					{
						Name:        "",
						DetectFiles: []string{"custom.json"},
						LockFiles:   []string{"custom.lock"},
						DepsDir:     "custom_modules",
						InstallCmd:  []string{"echo", "install"},
					},
				},
			},
			expectedPMs: nil,
			expectError: true,
		},
		{
			name: "invalid custom PM - missing detect files",
			globalCfg: &config.GlobalConfig{
				PackageManagers: []config.PackageManagerConfig{
					{
						Name:        "custom-pm",
						DetectFiles: []string{},
						LockFiles:   []string{"custom.lock"},
						DepsDir:     "custom_modules",
						InstallCmd:  []string{"echo", "install"},
					},
				},
			},
			expectedPMs: nil,
			expectError: true,
		},
		{
			name: "invalid custom PM - missing lock files",
			globalCfg: &config.GlobalConfig{
				PackageManagers: []config.PackageManagerConfig{
					{
						Name:        "custom-pm",
						DetectFiles: []string{"custom.json"},
						LockFiles:   []string{},
						DepsDir:     "custom_modules",
						InstallCmd:  []string{"echo", "install"},
					},
				},
			},
			expectedPMs: nil,
			expectError: true,
		},
		{
			name: "invalid custom PM - missing deps dir",
			globalCfg: &config.GlobalConfig{
				PackageManagers: []config.PackageManagerConfig{
					{
						Name:        "custom-pm",
						DetectFiles: []string{"custom.json"},
						LockFiles:   []string{"custom.lock"},
						DepsDir:     "",
						InstallCmd:  []string{"echo", "install"},
					},
				},
			},
			expectedPMs: nil,
			expectError: true,
		},
		{
			name: "invalid custom PM - missing install command",
			globalCfg: &config.GlobalConfig{
				PackageManagers: []config.PackageManagerConfig{
					{
						Name:        "custom-pm",
						DetectFiles: []string{"custom.json"},
						LockFiles:   []string{"custom.lock"},
						DepsDir:     "custom_modules",
						InstallCmd:  []string{},
					},
				},
			},
			expectedPMs: nil,
			expectError: true,
		},
		{
			name: "invalid custom PM - command not found",
			globalCfg: &config.GlobalConfig{
				PackageManagers: []config.PackageManagerConfig{
					{
						Name:        "custom-pm",
						DetectFiles: []string{"custom.json"},
						LockFiles:   []string{"custom.lock"},
						DepsDir:     "custom_modules",
						InstallCmd:  []string{"nonexistent-command-xyz123"},
					},
				},
			},
			expectedPMs: nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pms, err := services.LoadPackageManagers(tt.globalCfg)

			if tt.expectError {
				if err == nil {
					t.Errorf("LoadPackageManagers() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("LoadPackageManagers() unexpected error = %v", err)
			}

			// Check count
			if len(pms) != len(tt.expectedPMs) {
				t.Errorf("LoadPackageManagers() returned %d PMs, want %d", len(pms), len(tt.expectedPMs))
			}

			// Check all expected PMs are present
			foundPMs := make(map[string]bool)
			for _, pm := range pms {
				foundPMs[pm.Name] = true

				// Verify PM has required fields
				if len(pm.DetectFiles) == 0 {
					t.Errorf("PM %s has no detect files", pm.Name)
				}
				if len(pm.LockFiles) == 0 {
					t.Errorf("PM %s has no lock files", pm.Name)
				}
				if pm.DepsDir == "" {
					t.Errorf("PM %s has no deps dir", pm.Name)
				}
				if len(pm.InstallCmd) == 0 {
					t.Errorf("PM %s has no install command", pm.Name)
				}
			}

			for expectedPM := range tt.expectedPMs {
				if !foundPMs[expectedPM] {
					t.Errorf("Expected PM %s not found. Found: %v", expectedPM, foundPMs)
				}
			}

			// Check no unexpected PMs
			for foundPM := range foundPMs {
				if !tt.expectedPMs[foundPM] {
					t.Errorf("Unexpected PM %s found", foundPM)
				}
			}
		})
	}
}

func TestLoadPackageManagers_CustomPMWithMultipleLockFiles(t *testing.T) {
	globalCfg := &config.GlobalConfig{
		PackageManagers: []config.PackageManagerConfig{
			{
				Name:        "multi-lock-pm",
				DetectFiles: []string{"project.json"},
				LockFiles:   []string{"lock1.json", "lock2.json", "lock3.json"},
				DepsDir:     "deps",
				InstallCmd:  []string{"echo", "install"},
			},
		},
	}

	pms, err := services.LoadPackageManagers(globalCfg)
	if err != nil {
		t.Fatalf("LoadPackageManagers() error = %v", err)
	}

	// Find the custom PM
	var customPM *services.PackageManager
	for _, pm := range pms {
		if pm.Name == "multi-lock-pm" {
			customPM = &pm
			break
		}
	}

	if customPM == nil {
		t.Fatalf("Custom PM 'multi-lock-pm' not found")
	}

	// Verify multiple lockfiles are preserved
	if len(customPM.LockFiles) != 3 {
		t.Errorf("CustomPM has %d lockfiles, want 3", len(customPM.LockFiles))
	}

	expectedLockFiles := []string{"lock1.json", "lock2.json", "lock3.json"}
	for i, lockFile := range expectedLockFiles {
		if i >= len(customPM.LockFiles) || customPM.LockFiles[i] != lockFile {
			t.Errorf("CustomPM lockfile[%d] = %v, want %v", i, customPM.LockFiles[i], lockFile)
		}
	}
}

func TestLoadPackageManagers_CustomPMWithComplexInstallCmd(t *testing.T) {
	globalCfg := &config.GlobalConfig{
		PackageManagers: []config.PackageManagerConfig{
			{
				Name:        "complex-pm",
				DetectFiles: []string{"project.json"},
				LockFiles:   []string{"project.lock"},
				DepsDir:     "deps",
				InstallCmd:  []string{"sh", "-c", "echo setup && echo install"},
			},
		},
	}

	pms, err := services.LoadPackageManagers(globalCfg)
	if err != nil {
		t.Fatalf("LoadPackageManagers() error = %v", err)
	}

	// Find the custom PM
	var customPM *services.PackageManager
	for _, pm := range pms {
		if pm.Name == "complex-pm" {
			customPM = &pm
			break
		}
	}

	if customPM == nil {
		t.Fatalf("Custom PM 'complex-pm' not found")
	}

	// Verify complex install command is preserved
	expectedCmd := []string{"sh", "-c", "echo setup && echo install"}
	if len(customPM.InstallCmd) != len(expectedCmd) {
		t.Errorf("CustomPM has %d install cmd parts, want %d", len(customPM.InstallCmd), len(expectedCmd))
	}

	for i, part := range expectedCmd {
		if i >= len(customPM.InstallCmd) || customPM.InstallCmd[i] != part {
			t.Errorf("CustomPM installCmd[%d] = %v, want %v", i, customPM.InstallCmd[i], part)
		}
	}
}

func TestLoadPackageManagers_NilConfig(t *testing.T) {
	// Should return only built-in PMs when config is nil
	pms, err := services.LoadPackageManagers(nil)
	if err != nil {
		t.Fatalf("LoadPackageManagers(nil) error = %v", err)
	}

	// Should have exactly the built-in PMs
	expectedCount := 8 // npm, pnpm, yarn, go, pip, cargo, composer, bundler
	if len(pms) != expectedCount {
		t.Errorf("LoadPackageManagers(nil) returned %d PMs, want %d", len(pms), expectedCount)
	}
}
