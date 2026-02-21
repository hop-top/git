package hop_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"hop.top/git/internal/hop"
)

func TestGetDataHome(t *testing.T) {
	tests := []struct {
		name        string
		xdgDataHome string
		expectPath  func(home string) string
	}{
		{
			name:        "uses XDG_DATA_HOME when set",
			xdgDataHome: "/custom/data",
			expectPath:  func(home string) string { return "/custom/data" },
		},
		{
			name:        "uses OS defaults when XDG_DATA_HOME not set",
			xdgDataHome: "",
			expectPath: func(home string) string {
				switch runtime.GOOS {
				case "windows":
					if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
						return localAppData
					}
					return filepath.Join(home, "AppData", "Local")
				case "darwin":
					return filepath.Join(home, "Library", "Application Support")
				default:
					return filepath.Join(home, ".local", "share")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original env
			origXDG := os.Getenv("XDG_DATA_HOME")
			defer os.Setenv("XDG_DATA_HOME", origXDG)

			// Set test env
			if tt.xdgDataHome != "" {
				os.Setenv("XDG_DATA_HOME", tt.xdgDataHome)
			} else {
				os.Unsetenv("XDG_DATA_HOME")
			}

			// Get result
			result := hop.GetDataHome()

			// Get expected
			home, _ := os.UserHomeDir()
			expected := tt.expectPath(home)

			if result != expected {
				t.Errorf("GetDataHome() = %v, want %v", result, expected)
			}
		})
	}
}

func TestGetGitHopDataHome(t *testing.T) {
	tests := []struct {
		name           string
		gitHopDataHome string
		xdgDataHome    string
		expectContains string
	}{
		{
			name:           "uses GIT_HOP_DATA_HOME when set",
			gitHopDataHome: "/custom/git-hop-data",
			xdgDataHome:    "",
			expectContains: "/custom/git-hop-data",
		},
		{
			name:           "falls back to XDG_DATA_HOME/git-hop",
			gitHopDataHome: "",
			xdgDataHome:    "/custom/data",
			expectContains: "/custom/data/git-hop",
		},
		{
			name:           "uses OS defaults when no env vars set",
			gitHopDataHome: "",
			xdgDataHome:    "",
			expectContains: "git-hop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original env
			origGitHop := os.Getenv("GIT_HOP_DATA_HOME")
			origXDG := os.Getenv("XDG_DATA_HOME")
			defer func() {
				os.Setenv("GIT_HOP_DATA_HOME", origGitHop)
				os.Setenv("XDG_DATA_HOME", origXDG)
			}()

			// Set test env
			if tt.gitHopDataHome != "" {
				os.Setenv("GIT_HOP_DATA_HOME", tt.gitHopDataHome)
			} else {
				os.Unsetenv("GIT_HOP_DATA_HOME")
			}

			if tt.xdgDataHome != "" {
				os.Setenv("XDG_DATA_HOME", tt.xdgDataHome)
			} else {
				os.Unsetenv("XDG_DATA_HOME")
			}

			// Get result
			result := hop.GetGitHopDataHome()

			// Verify
			if tt.gitHopDataHome != "" {
				if result != tt.gitHopDataHome {
					t.Errorf("GetGitHopDataHome() = %v, want %v", result, tt.gitHopDataHome)
				}
			} else if tt.xdgDataHome != "" {
				expected := filepath.Join(tt.xdgDataHome, "git-hop")
				if result != expected {
					t.Errorf("GetGitHopDataHome() = %v, want %v", result, expected)
				}
			} else {
				// Should contain git-hop somewhere in the path
				if !filepath.IsAbs(result) {
					t.Errorf("GetGitHopDataHome() should return absolute path, got %v", result)
				}
				if filepath.Base(result) != "git-hop" {
					t.Errorf("GetGitHopDataHome() should end with 'git-hop', got %v", result)
				}
			}
		})
	}
}

func TestGetConfigHome(t *testing.T) {
	// Save original
	origXDG := os.Getenv("XDG_CONFIG_HOME")
	defer os.Setenv("XDG_CONFIG_HOME", origXDG)

	// Test with XDG_CONFIG_HOME set
	os.Setenv("XDG_CONFIG_HOME", "/custom/config")
	result := hop.GetConfigHome()
	if result != "/custom/config" {
		t.Errorf("GetConfigHome() with XDG_CONFIG_HOME = %v, want /custom/config", result)
	}

	// Test with XDG_CONFIG_HOME unset
	os.Unsetenv("XDG_CONFIG_HOME")
	result = hop.GetConfigHome()

	// Should use os.UserConfigDir() or fallback to ~/.config
	if !filepath.IsAbs(result) {
		t.Errorf("GetConfigHome() should return absolute path, got %v", result)
	}
}

func TestGetCacheHome(t *testing.T) {
	// Save original
	origXDG := os.Getenv("XDG_CACHE_HOME")
	defer os.Setenv("XDG_CACHE_HOME", origXDG)

	// Test with XDG_CACHE_HOME set
	os.Setenv("XDG_CACHE_HOME", "/custom/cache")
	result := hop.GetCacheHome()
	if result != "/custom/cache" {
		t.Errorf("GetCacheHome() with XDG_CACHE_HOME = %v, want /custom/cache", result)
	}

	// Test with XDG_CACHE_HOME unset
	os.Unsetenv("XDG_CACHE_HOME")
	result = hop.GetCacheHome()

	// Should use os.UserCacheDir() or fallback
	if !filepath.IsAbs(result) {
		t.Errorf("GetCacheHome() should return absolute path, got %v", result)
	}
}

func TestGetHopspacePath(t *testing.T) {
	dataHome := "/data"
	org := "test-org"
	repo := "test-repo"

	result := hop.GetHopspacePath(dataHome, org, repo)
	expected := filepath.Join("/data", "test-org", "test-repo")

	if result != expected {
		t.Errorf("GetHopspacePath() = %v, want %v", result, expected)
	}
}

func TestPathsIntegration(t *testing.T) {
	// This test verifies the full path resolution chain works correctly

	// Save original env
	origGitHop := os.Getenv("GIT_HOP_DATA_HOME")
	defer os.Setenv("GIT_HOP_DATA_HOME", origGitHop)

	// Unset to test default behavior
	os.Unsetenv("GIT_HOP_DATA_HOME")

	dataHome := hop.GetGitHopDataHome()
	hopspacePath := hop.GetHopspacePath(dataHome, "org", "repo")

	// Verify structure
	if !filepath.IsAbs(hopspacePath) {
		t.Errorf("hopspacePath should be absolute, got %v", hopspacePath)
	}

	// Should end with org/repo
	if filepath.Base(hopspacePath) != "repo" {
		t.Errorf("hopspacePath should end with repo, got %v", hopspacePath)
	}

	if filepath.Base(filepath.Dir(hopspacePath)) != "org" {
		t.Errorf("hopspacePath parent should be org, got %v", filepath.Dir(hopspacePath))
	}
}
