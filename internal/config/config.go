package config

import (
	"time"
)

// HubConfig represents <hub>/hop.json
type HubConfig struct {
	Repo     RepoConfig           `json:"repo"`
	Branches map[string]HubBranch `json:"branches"`
	Settings HubSettings          `json:"settings"`
	Migrated bool                 `json:"migrated"` // True if migrated to registry
}

type RepoConfig struct {
	URI           string `json:"uri"`
	Org           string `json:"org"`
	Repo          string `json:"repo"`
	DefaultBranch string `json:"defaultBranch"`
}

type HubBranch struct {
	Path           string  `json:"path"`
	HopspaceBranch string  `json:"hopspaceBranch"`
	Fork           *string `json:"fork,omitempty"`
}

type HubSettings struct {
	CompareBranch *string  `json:"compareBranch,omitempty"`
	EnvPatterns   []string `json:"envPatterns"`
}

// HopspaceConfig represents $GIT_HOP_DATA_HOME/<org>/<repo>/hop.json
type HopspaceConfig struct {
	Repo     RepoConfig                `json:"repo"`
	Branches map[string]HopspaceBranch `json:"branches"`
	Forks    map[string]HopspaceFork   `json:"forks"`
}

type HopspaceBranch struct {
	Exists   bool      `json:"exists"`
	Path     string    `json:"path"`
	LastSync time.Time `json:"lastSync"`
}

type HopspaceFork struct {
	URI      string                    `json:"uri"`
	Org      string                    `json:"org"`
	Repo     string                    `json:"repo"`
	Branches map[string]HopspaceBranch `json:"branches"`
}

// PortsConfig represents ports.json
type PortsConfig struct {
	AllocationMode string                 `json:"allocationMode"`
	BaseRange      PortRange              `json:"baseRange"`
	Branches       map[string]BranchPorts `json:"branches"`
	Services       []string               `json:"services"`
}

type PortRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type BranchPorts struct {
	Ports map[string]int `json:"ports"`
}

// VolumesConfig represents volumes.json
type VolumesConfig struct {
	BasePath string                   `json:"basePath"`
	Branches map[string]BranchVolumes `json:"branches"`
	Cleanup  VolumeCleanup            `json:"cleanup"`
}

type BranchVolumes struct {
	Volumes map[string]string `json:"volumes"`
}

type VolumeCleanup struct {
	Orphaned            string `json:"orphaned"`
	UnusedThresholdDays int    `json:"unusedThresholdDays"`
}

// HopsConfig represents $XDG_CONFIG_HOME/git-hop/hops.json
type HopsConfig struct {
	Hops map[string]HopEntry `json:"hops"` // key: "org/repo:branch"
}

// HopEntry represents a single managed worktree
type HopEntry struct {
	Repo              string    `json:"repo"`        // "org/repo"
	Branch            string    `json:"branch"`      // "main"
	Path              string    `json:"path"`        // Absolute path to worktree
	ProjectRoot       string    `json:"projectRoot"` // Absolute path to project root (bare repo)
	AddedAt           time.Time `json:"addedAt"`
	LastSeen          time.Time `json:"lastSeen"`
	EnvState          string    `json:"envState"` // "up", "down", "none"
	HasDockerEnv      bool      `json:"hasDockerEnv"`
	FollowsConvention bool      `json:"followsConvention"` // Whether worktree follows naming convention
}

// GlobalConfig represents $XDG_CONFIG_HOME/git-hop/global.json
type GlobalConfig struct {
	Defaults        DefaultSettings          `json:"defaults"`
	PackageManagers []PackageManagerConfig   `json:"packageManagers,omitempty"`
	Backup          BackupSettings           `json:"backup,omitempty"`
	Conversion      ConversionSettings       `json:"conversion,omitempty"`
}

// PackageManagerConfig represents a custom package manager configuration
type PackageManagerConfig struct {
	Name        string   `json:"name"`
	DetectFiles []string `json:"detectFiles"`
	LockFiles   []string `json:"lockFiles"`
	DepsDir     string   `json:"depsDir"`
	InstallCmd  []string `json:"installCmd"`
}

// DefaultSettings represents global default settings
type DefaultSettings struct {
	AutoEnvStart              bool `json:"autoEnvStart"`
	ShowAllManagedRepos       bool `json:"showAllManagedRepos"`
	UnusedThresholdDays       int  `json:"unusedThresholdDays"`
	BareRepo                  bool `json:"bareRepo"`
	EnforceCleanForConversion bool `json:"enforceCleanForConversion"`
	ConventionWarning         bool `json:"conventionWarning"`
	GitDomain                 string `json:"gitDomain"`
	WorktreeLocation          string `json:"worktreeLocation,omitempty"`
}

// UserConfig represents global config.json (legacy)
type UserConfig struct {
	Defaults UserDefaults `json:"defaults"`
	Paths    UserPaths    `json:"paths"`
}

type UserDefaults struct {
	CompareBranch  *string  `json:"compareBranch,omitempty"`
	EnvPatterns    []string `json:"envPatterns"`
	AllocationMode string   `json:"allocationMode"`
}

type UserPaths struct {
	DataHome  *string `json:"dataHome,omitempty"`
	CacheHome *string `json:"cacheHome,omitempty"`
}

type StructureType string

const (
	StandardRepo     StructureType = "standard"
	BareWorktreeRoot StructureType = "bare-worktree"
	WorktreeRoot     StructureType = "worktree"
	WorktreeChild    StructureType = "worktree-child"
	NotGit           StructureType = "not-git"
	UnknownStructure StructureType = "unknown"
)

type BackupMetadata struct {
	Timestamp     time.Time `json:"timestamp"`
	OriginalPath  string    `json:"originalPath"`
	RemoteUrl     string    `json:"remoteUrl"`
	CurrentBranch string    `json:"currentBranch"`
	Structure     string    `json:"structure"`
	HasStashes    bool      `json:"hasStashes"`
	StashCount    int       `json:"stashCount"`
	GitStatus     string    `json:"gitStatus"`
}

type ConversionResult struct {
	Success      bool            `json:"success"`
	BackupPath   string          `json:"backupPath"`
	ProjectPath  string          `json:"projectPath"`
	Errors       []string        `json:"errors,omitempty"`
	Warnings     []string        `json:"warnings,omitempty"`
	Metadata     *BackupMetadata `json:"metadata,omitempty"`
	CreatedFiles []string        `json:"createdFiles"`
	ModifiedDirs []string        `json:"modifiedDirs"`
}

type BackupSettings struct {
	Enabled         bool `json:"enabled"`
	KeepBackup      bool `json:"keepBackup"`
	MaxBackups      int  `json:"maxBackups"`
	CleanupAgeDays  int  `json:"cleanupAgeDays"`
	PreserveStashes bool `json:"preserveStashes"`
}

type ConversionSettings struct {
	EnforceClean    bool `json:"enforceClean"`
	AllowDirtyForce bool `json:"allowDirtyForce"`
	AutoRollback    bool `json:"autoRollback"`
}
