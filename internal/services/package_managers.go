package services

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"hop.top/git/internal/config"
	"github.com/spf13/afero"
)

// ErrBinaryNotFound is returned by Install when the package manager binary is not found in PATH.
// Callers may choose to skip installation rather than treat this as a hard error.
var ErrBinaryNotFound = fmt.Errorf("package manager binary not found in PATH")

// PackageManager represents a package manager configuration
type PackageManager struct {
	Name        string
	DetectFiles []string // Files indicating PM presence
	LockFiles   []string // Lockfile paths (in priority order)
	DepsDir     string   // Where deps get installed
	InstallCmd  []string // Install command
}

// HashLockfile computes SHA256 hash of lockfile (first 6 chars)
func (pm *PackageManager) HashLockfile(fs afero.Fs, lockfilePath string) (string, error) {
	file, err := fs.Open(lockfilePath)
	if err != nil {
		return "", fmt.Errorf("failed to open lockfile: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to hash lockfile: %w", err)
	}

	fullHash := fmt.Sprintf("%x", hash.Sum(nil))
	return fullHash[:6], nil
}

// HashLockfileLong computes SHA256 hash of lockfile (first 12 chars for collision handling)
func (pm *PackageManager) HashLockfileLong(fs afero.Fs, lockfilePath string) (string, error) {
	file, err := fs.Open(lockfilePath)
	if err != nil {
		return "", fmt.Errorf("failed to open lockfile: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("failed to hash lockfile: %w", err)
	}

	fullHash := fmt.Sprintf("%x", hash.Sum(nil))
	return fullHash[:12], nil
}

// Install runs the install command in the target directory.
// Returns ErrBinaryNotFound if the package manager binary is not in PATH.
func (pm *PackageManager) Install(targetDir string, worktreePath string) error {
	if len(pm.InstallCmd) == 0 {
		return fmt.Errorf("no install command defined for %s", pm.Name)
	}

	// Check binary availability before attempting to run; avoids confusing
	// "exec: not found" errors and lets callers skip gracefully.
	if pm.Name != "pip" {
		if _, err := exec.LookPath(pm.InstallCmd[0]); err != nil {
			return fmt.Errorf("%w: %s", ErrBinaryNotFound, pm.InstallCmd[0])
		}
	}

	// Special handling for pip - needs venv creation
	if pm.Name == "pip" {
		// Create venv in target directory
		venvCmd := exec.Command("python", "-m", "venv", targetDir)
		venvCmd.Dir = worktreePath
		if err := venvCmd.Run(); err != nil {
			return fmt.Errorf("failed to create venv: %w", err)
		}

		// Install requirements
		pipPath := filepath.Join(targetDir, "bin", "pip")
		reqPath := filepath.Join(worktreePath, "requirements.txt")
		installCmd := exec.Command(pipPath, "install", "-r", reqPath)
		installCmd.Dir = worktreePath
		if err := installCmd.Run(); err != nil {
			return fmt.Errorf("failed to install pip deps: %w", err)
		}
		return nil
	}

	// For other package managers, run install command
	cmd := exec.Command(pm.InstallCmd[0], pm.InstallCmd[1:]...)
	cmd.Dir = worktreePath
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run install command for %s: %w", pm.Name, err)
	}

	return nil
}

// Validate checks if the package manager configuration is valid
func (pm *PackageManager) Validate() error {
	if pm.Name == "" {
		return fmt.Errorf("package manager name is required")
	}
	if len(pm.DetectFiles) == 0 {
		return fmt.Errorf("package manager %s: detectFiles is required", pm.Name)
	}
	if len(pm.LockFiles) == 0 {
		return fmt.Errorf("package manager %s: lockFiles is required", pm.Name)
	}
	if pm.DepsDir == "" {
		return fmt.Errorf("package manager %s: depsDir is required", pm.Name)
	}
	if len(pm.InstallCmd) == 0 {
		return fmt.Errorf("package manager %s: installCmd is required", pm.Name)
	}

	// Check if install command is available
	if _, err := exec.LookPath(pm.InstallCmd[0]); err != nil {
		return fmt.Errorf("package manager %s: command %s not found in PATH", pm.Name, pm.InstallCmd[0])
	}

	return nil
}

// getBuiltInPackageManagers returns the built-in package manager definitions
func getBuiltInPackageManagers() []PackageManager {
	return []PackageManager{
		{
			Name:        "npm",
			DetectFiles: []string{"package.json"},
			LockFiles:   []string{"package-lock.json", "npm-shrinkwrap.json"},
			DepsDir:     "node_modules",
			InstallCmd:  []string{"npm", "ci"},
		},
		{
			Name:        "pnpm",
			DetectFiles: []string{"pnpm-lock.yaml"},
			LockFiles:   []string{"pnpm-lock.yaml"},
			DepsDir:     "node_modules",
			InstallCmd:  []string{"pnpm", "install", "--frozen-lockfile"},
		},
		{
			Name:        "yarn",
			DetectFiles: []string{"yarn.lock"},
			LockFiles:   []string{"yarn.lock"},
			DepsDir:     "node_modules",
			InstallCmd:  []string{"yarn", "install", "--frozen-lockfile"},
		},
		{
			Name:        "go",
			DetectFiles: []string{"go.mod"},
			LockFiles:   []string{"go.sum"},
			DepsDir:     "vendor",
			InstallCmd:  []string{"sh", "-c", "go mod download && go mod vendor"},
		},
		{
			Name:        "pip",
			DetectFiles: []string{"requirements.txt", "setup.py"},
			LockFiles:   []string{"requirements.txt"},
			DepsDir:     "venv",
			InstallCmd:  []string{"pip", "install", "-r", "requirements.txt"},
		},
		{
			Name:        "cargo",
			DetectFiles: []string{"Cargo.toml"},
			LockFiles:   []string{"Cargo.lock"},
			DepsDir:     "target",
			InstallCmd:  []string{"cargo", "fetch"},
		},
		{
			Name:        "composer",
			DetectFiles: []string{"composer.json"},
			LockFiles:   []string{"composer.lock"},
			DepsDir:     "vendor",
			InstallCmd:  []string{"composer", "install", "--no-dev"},
		},
		{
			Name:        "bundler",
			DetectFiles: []string{"Gemfile"},
			LockFiles:   []string{"Gemfile.lock"},
			DepsDir:     "vendor/bundle",
			InstallCmd:  []string{"bundle", "install", "--deployment"},
		},
	}
}

// LoadPackageManagers loads built-in and custom package managers from config
func LoadPackageManagers(globalConfig *config.GlobalConfig) ([]PackageManager, error) {
	// Start with built-in package managers
	pms := getBuiltInPackageManagers()
	pmMap := make(map[string]PackageManager)
	for _, pm := range pms {
		pmMap[pm.Name] = pm
	}

	// Load custom package managers from config (overrides built-in if same name)
	if globalConfig != nil && len(globalConfig.PackageManagers) > 0 {
		for _, customPM := range globalConfig.PackageManagers {
			pm := PackageManager{
				Name:        customPM.Name,
				DetectFiles: customPM.DetectFiles,
				LockFiles:   customPM.LockFiles,
				DepsDir:     customPM.DepsDir,
				InstallCmd:  customPM.InstallCmd,
			}

			// Validate custom PM
			if err := pm.Validate(); err != nil {
				return nil, fmt.Errorf("invalid custom package manager: %w", err)
			}

			// Override built-in or add new
			pmMap[pm.Name] = pm
		}
	}

	// Convert map back to slice
	result := make([]PackageManager, 0, len(pmMap))
	for _, pm := range pmMap {
		result = append(result, pm)
	}

	return result, nil
}

// DetectPackageManagers detects all package managers present in a worktree
func DetectPackageManagers(fs afero.Fs, worktreePath string, availablePMs []PackageManager) ([]PackageManager, error) {
	detected := make([]PackageManager, 0)

	for _, pm := range availablePMs {
		// Check if any detect files exist
		found := false
		for _, detectFile := range pm.DetectFiles {
			filePath := filepath.Join(worktreePath, detectFile)
			exists, err := afero.Exists(fs, filePath)
			if err != nil {
				return nil, fmt.Errorf("failed to check for %s: %w", detectFile, err)
			}
			if exists {
				found = true
				break
			}
		}

		if found {
			// Verify at least one lockfile exists
			hasLockfile := false
			for _, lockFile := range pm.LockFiles {
				lockPath := filepath.Join(worktreePath, lockFile)
				exists, err := afero.Exists(fs, lockPath)
				if err != nil {
					continue
				}
				if exists {
					hasLockfile = true
					break
				}
			}

			if hasLockfile {
				detected = append(detected, pm)
			}
		}
	}

	return detected, nil
}

// FindLockfile finds the first existing lockfile from the PM's lockfile list
func (pm *PackageManager) FindLockfile(fs afero.Fs, worktreePath string) (string, error) {
	for _, lockFile := range pm.LockFiles {
		lockPath := filepath.Join(worktreePath, lockFile)
		exists, err := afero.Exists(fs, lockPath)
		if err != nil {
			continue
		}
		if exists {
			return lockPath, nil
		}
	}
	return "", fmt.Errorf("no lockfile found for %s in %s", pm.Name, worktreePath)
}

// GetDepsKey returns the deps directory key (e.g., "node_modules.abc123")
func (pm *PackageManager) GetDepsKey(hash string) string {
	// Remove path separators from depsDir for the key
	depsName := strings.ReplaceAll(pm.DepsDir, string(filepath.Separator), "_")
	return fmt.Sprintf("%s.%s", depsName, hash)
}

// ApplyOverride creates a new PackageManager with overridden install command
func (pm *PackageManager) ApplyOverride(override *config.PackageManagerOverride) *PackageManager {
	if override == nil || len(override.InstallCmd) == 0 {
		return pm
	}

	// Create a copy with overridden install command
	overridden := *pm
	overridden.InstallCmd = override.InstallCmd
	return &overridden
}

// ResolveInstallCmd resolves the install command with hierarchy:
// 1. Branch-level override (highest priority)
// 2. Repo-level override
// 3. Global package manager config (fallback)
func ResolveInstallCmd(pm PackageManager, hopspaceConfig *config.HopspaceConfig, branch string) *PackageManager {
	if hopspaceConfig == nil {
		return &pm
	}

	// Check branch-level override first
	if branchConfig, exists := hopspaceConfig.Branches[branch]; exists {
		if override, hasOverride := branchConfig.PackageManagers[pm.Name]; hasOverride {
			return pm.ApplyOverride(&override)
		}
	}

	// Check repo-level override
	if override, hasOverride := hopspaceConfig.PackageManagers[pm.Name]; hasOverride {
		return pm.ApplyOverride(&override)
	}

	// Return original (uses global config)
	return &pm
}
