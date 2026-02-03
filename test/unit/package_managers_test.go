package hop_test

import (
	"testing"

	"github.com/jadb/git-hop/internal/services"
	"github.com/spf13/afero"
)

func TestDetectPackageManagers_BuiltIn(t *testing.T) {
	tests := []struct {
		name          string
		files         map[string]string
		expectedPMs   []string
		expectedCount int
	}{
		{
			name: "detects npm with package.json and package-lock.json",
			files: map[string]string{
				"package.json":      `{"name": "test"}`,
				"package-lock.json": `{"lockfileVersion": 2}`,
			},
			expectedPMs:   []string{"npm"},
			expectedCount: 1,
		},
		{
			name: "detects pnpm with pnpm-lock.yaml",
			files: map[string]string{
				"package.json":   `{"name": "test"}`,
				"pnpm-lock.yaml": `lockfileVersion: 5.4`,
			},
			expectedPMs:   []string{"pnpm"},
			expectedCount: 1,
		},
		{
			name: "detects yarn with yarn.lock",
			files: map[string]string{
				"package.json": `{"name": "test"}`,
				"yarn.lock":    `# yarn lockfile v1`,
			},
			expectedPMs:   []string{"yarn"},
			expectedCount: 1,
		},
		{
			name: "detects go with go.mod and go.sum",
			files: map[string]string{
				"go.mod": `module example.com/test`,
				"go.sum": `github.com/example/dep v1.0.0 h1:abc`,
			},
			expectedPMs:   []string{"go"},
			expectedCount: 1,
		},
		{
			name: "detects pip with requirements.txt",
			files: map[string]string{
				"requirements.txt": `django==3.2.0`,
			},
			expectedPMs:   []string{"pip"},
			expectedCount: 1,
		},
		{
			name: "detects pip with setup.py",
			files: map[string]string{
				"setup.py":         `from setuptools import setup`,
				"requirements.txt": `django==3.2.0`,
			},
			expectedPMs:   []string{"pip"},
			expectedCount: 1,
		},
		{
			name: "detects cargo with Cargo.toml and Cargo.lock",
			files: map[string]string{
				"Cargo.toml": `[package]\nname = "test"`,
				"Cargo.lock": `# cargo lock`,
			},
			expectedPMs:   []string{"cargo"},
			expectedCount: 1,
		},
		{
			name: "detects composer with composer.json and composer.lock",
			files: map[string]string{
				"composer.json": `{"name": "vendor/package"}`,
				"composer.lock": `{"packages": []}`,
			},
			expectedPMs:   []string{"composer"},
			expectedCount: 1,
		},
		{
			name: "detects bundler with Gemfile and Gemfile.lock",
			files: map[string]string{
				"Gemfile":      `source "https://rubygems.org"`,
				"Gemfile.lock": `GEM\n  remote: https://rubygems.org`,
			},
			expectedPMs:   []string{"bundler"},
			expectedCount: 1,
		},
		{
			name: "npm requires lockfile",
			files: map[string]string{
				"package.json": `{"name": "test"}`,
			},
			expectedPMs:   []string{},
			expectedCount: 0,
		},
		{
			name: "go requires both go.mod and go.sum",
			files: map[string]string{
				"go.mod": `module example.com/test`,
			},
			expectedPMs:   []string{},
			expectedCount: 0,
		},
		{
			name:          "empty worktree detects nothing",
			files:         map[string]string{},
			expectedPMs:   []string{},
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create in-memory filesystem
			fs := afero.NewMemMapFs()
			worktreePath := "/test/worktree"

			// Create worktree directory
			if err := fs.MkdirAll(worktreePath, 0755); err != nil {
				t.Fatalf("failed to create worktree: %v", err)
			}

			// Create test files
			for filename, content := range tt.files {
				if err := afero.WriteFile(fs, worktreePath+"/"+filename, []byte(content), 0644); err != nil {
					t.Fatalf("failed to create file %s: %v", filename, err)
				}
			}

			// Load built-in package managers
			pms, err := services.LoadPackageManagers(nil)
			if err != nil {
				t.Fatalf("LoadPackageManagers() error = %v", err)
			}

			// Detect package managers
			detected, err := services.DetectPackageManagers(fs, worktreePath, pms)
			if err != nil {
				t.Fatalf("DetectPackageManagers() error = %v", err)
			}

			// Check count
			if len(detected) != tt.expectedCount {
				t.Errorf("DetectPackageManagers() detected %d PMs, want %d", len(detected), tt.expectedCount)
			}

			// Check expected PMs are present
			detectedMap := make(map[string]bool)
			for _, pm := range detected {
				detectedMap[pm.Name] = true
			}

			for _, expectedPM := range tt.expectedPMs {
				if !detectedMap[expectedPM] {
					t.Errorf("Expected PM %s not detected. Detected: %v", expectedPM, detectedMap)
				}
			}
		})
	}
}

func TestDetectPackageManagers_MultiPM(t *testing.T) {
	// Create in-memory filesystem
	fs := afero.NewMemMapFs()
	worktreePath := "/test/worktree"

	// Create worktree directory
	if err := fs.MkdirAll(worktreePath, 0755); err != nil {
		t.Fatalf("failed to create worktree: %v", err)
	}

	// Create files for multiple package managers
	files := map[string]string{
		"package.json":      `{"name": "test"}`,
		"package-lock.json": `{"lockfileVersion": 2}`,
		"go.mod":            `module example.com/test`,
		"go.sum":            `github.com/example/dep v1.0.0 h1:abc`,
		"requirements.txt":  `django==3.2.0`,
	}

	for filename, content := range files {
		if err := afero.WriteFile(fs, worktreePath+"/"+filename, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create file %s: %v", filename, err)
		}
	}

	// Load built-in package managers
	pms, err := services.LoadPackageManagers(nil)
	if err != nil {
		t.Fatalf("LoadPackageManagers() error = %v", err)
	}

	// Detect package managers
	detected, err := services.DetectPackageManagers(fs, worktreePath, pms)
	if err != nil {
		t.Fatalf("DetectPackageManagers() error = %v", err)
	}

	// Should detect npm, go, and pip
	if len(detected) != 3 {
		t.Errorf("DetectPackageManagers() detected %d PMs, want 3", len(detected))
	}

	expectedPMs := map[string]bool{
		"npm": true,
		"go":  true,
		"pip": true,
	}

	for _, pm := range detected {
		if !expectedPMs[pm.Name] {
			t.Errorf("Unexpected PM detected: %s", pm.Name)
		}
		delete(expectedPMs, pm.Name)
	}

	if len(expectedPMs) > 0 {
		t.Errorf("Expected PMs not detected: %v", expectedPMs)
	}
}

func TestLoadPackageManagers_BuiltInOnly(t *testing.T) {
	// Load with nil config (built-in only)
	pms, err := services.LoadPackageManagers(nil)
	if err != nil {
		t.Fatalf("LoadPackageManagers() error = %v", err)
	}

	// Check that we have the expected built-in PMs
	expectedPMs := map[string]bool{
		"npm":      true,
		"pnpm":     true,
		"yarn":     true,
		"go":       true,
		"pip":      true,
		"cargo":    true,
		"composer": true,
		"bundler":  true,
	}

	if len(pms) != len(expectedPMs) {
		t.Errorf("LoadPackageManagers() returned %d PMs, want %d", len(pms), len(expectedPMs))
	}

	for _, pm := range pms {
		if !expectedPMs[pm.Name] {
			t.Errorf("Unexpected built-in PM: %s", pm.Name)
		}

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
}

func TestPackageManager_FindLockfile(t *testing.T) {
	tests := []struct {
		name          string
		pm            services.PackageManager
		files         []string
		expectedFile  string
		expectError   bool
	}{
		{
			name: "finds first priority lockfile",
			pm: services.PackageManager{
				Name:      "npm",
				LockFiles: []string{"package-lock.json", "npm-shrinkwrap.json"},
			},
			files:        []string{"package-lock.json", "npm-shrinkwrap.json"},
			expectedFile: "package-lock.json",
			expectError:  false,
		},
		{
			name: "finds second priority lockfile when first is missing",
			pm: services.PackageManager{
				Name:      "npm",
				LockFiles: []string{"package-lock.json", "npm-shrinkwrap.json"},
			},
			files:        []string{"npm-shrinkwrap.json"},
			expectedFile: "npm-shrinkwrap.json",
			expectError:  false,
		},
		{
			name: "returns error when no lockfile found",
			pm: services.PackageManager{
				Name:      "npm",
				LockFiles: []string{"package-lock.json", "npm-shrinkwrap.json"},
			},
			files:        []string{},
			expectedFile: "",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create in-memory filesystem
			fs := afero.NewMemMapFs()
			worktreePath := "/test/worktree"

			// Create worktree directory
			if err := fs.MkdirAll(worktreePath, 0755); err != nil {
				t.Fatalf("failed to create worktree: %v", err)
			}

			// Create test files
			for _, filename := range tt.files {
				if err := afero.WriteFile(fs, worktreePath+"/"+filename, []byte("test"), 0644); err != nil {
					t.Fatalf("failed to create file %s: %v", filename, err)
				}
			}

			// Find lockfile
			lockfile, err := tt.pm.FindLockfile(fs, worktreePath)

			if tt.expectError {
				if err == nil {
					t.Errorf("FindLockfile() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("FindLockfile() unexpected error = %v", err)
				}
				expectedPath := worktreePath + "/" + tt.expectedFile
				if lockfile != expectedPath {
					t.Errorf("FindLockfile() = %v, want %v", lockfile, expectedPath)
				}
			}
		})
	}
}

func TestPackageManager_GetDepsKey(t *testing.T) {
	tests := []struct {
		name        string
		pm          services.PackageManager
		hash        string
		expectedKey string
	}{
		{
			name: "npm generates correct key",
			pm: services.PackageManager{
				Name:    "npm",
				DepsDir: "node_modules",
			},
			hash:        "abc123",
			expectedKey: "node_modules.abc123",
		},
		{
			name: "go generates correct key",
			pm: services.PackageManager{
				Name:    "go",
				DepsDir: "vendor",
			},
			hash:        "def456",
			expectedKey: "vendor.def456",
		},
		{
			name: "bundler with nested path generates correct key",
			pm: services.PackageManager{
				Name:    "bundler",
				DepsDir: "vendor/bundle",
			},
			hash:        "xyz789",
			expectedKey: "vendor_bundle.xyz789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := tt.pm.GetDepsKey(tt.hash)
			if key != tt.expectedKey {
				t.Errorf("GetDepsKey(%s) = %v, want %v", tt.hash, key, tt.expectedKey)
			}
		})
	}
}

func TestPackageManager_Validate(t *testing.T) {
	tests := []struct {
		name        string
		pm          services.PackageManager
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid PM passes validation",
			pm: services.PackageManager{
				Name:        "test",
				DetectFiles: []string{"test.json"},
				LockFiles:   []string{"test.lock"},
				DepsDir:     "test_modules",
				InstallCmd:  []string{"echo", "test"},
			},
			expectError: false,
		},
		{
			name: "missing name fails validation",
			pm: services.PackageManager{
				Name:        "",
				DetectFiles: []string{"test.json"},
				LockFiles:   []string{"test.lock"},
				DepsDir:     "test_modules",
				InstallCmd:  []string{"echo", "test"},
			},
			expectError: true,
			errorMsg:    "name is required",
		},
		{
			name: "missing detect files fails validation",
			pm: services.PackageManager{
				Name:        "test",
				DetectFiles: []string{},
				LockFiles:   []string{"test.lock"},
				DepsDir:     "test_modules",
				InstallCmd:  []string{"echo", "test"},
			},
			expectError: true,
			errorMsg:    "detectFiles is required",
		},
		{
			name: "missing lock files fails validation",
			pm: services.PackageManager{
				Name:        "test",
				DetectFiles: []string{"test.json"},
				LockFiles:   []string{},
				DepsDir:     "test_modules",
				InstallCmd:  []string{"echo", "test"},
			},
			expectError: true,
			errorMsg:    "lockFiles is required",
		},
		{
			name: "missing deps dir fails validation",
			pm: services.PackageManager{
				Name:        "test",
				DetectFiles: []string{"test.json"},
				LockFiles:   []string{"test.lock"},
				DepsDir:     "",
				InstallCmd:  []string{"echo", "test"},
			},
			expectError: true,
			errorMsg:    "depsDir is required",
		},
		{
			name: "missing install command fails validation",
			pm: services.PackageManager{
				Name:        "test",
				DetectFiles: []string{"test.json"},
				LockFiles:   []string{"test.lock"},
				DepsDir:     "test_modules",
				InstallCmd:  []string{},
			},
			expectError: true,
			errorMsg:    "installCmd is required",
		},
		{
			name: "nonexistent command fails validation",
			pm: services.PackageManager{
				Name:        "test",
				DetectFiles: []string{"test.json"},
				LockFiles:   []string{"test.lock"},
				DepsDir:     "test_modules",
				InstallCmd:  []string{"nonexistent-command-xyz123"},
			},
			expectError: true,
			errorMsg:    "not found in PATH",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.pm.Validate()

			if tt.expectError {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.errorMsg)
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
