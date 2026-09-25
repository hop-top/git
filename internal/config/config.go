package config

import (
	"path/filepath"
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
	// Mode says where the hub keeps its hopspace. RepoModeGlobal (set by
	// clone --global) means $GIT_HOP_DATA_HOME/<org>/<repo>; empty means
	// the hub's own hop.json, the default.
	Mode string `json:"mode,omitempty"`
}

// RepoModeGlobal marks a hub whose hopspace lives in the data home.
const RepoModeGlobal = "global"

type HubBranch struct {
	Path           string  `json:"path"`
	HopspaceBranch string  `json:"hopspaceBranch"`
	Fork           *string `json:"fork,omitempty"`
	// Base is the branch this worktree was forked from, used as the
	// comparison target for "ahead/behind" and "merged" labels in
	// `git hop status` and `git hop list`. nil means "fall back to the
	// hub's compare branch and then DefaultBranch" — the historical
	// behavior. Set explicitly when the worktree was branched from a
	// non-default base (e.g. a long-lived integration branch or a
	// stacked feature branch); recorded by `git hop add` at creation
	// time and back-filled for legacy worktrees by `git hop repair
	// --base`.
	Base *string `json:"base,omitempty"`
	// Task is the id of the task this worktree was added for (`git hop
	// add --task`). Metadata only: it never shapes the branch name.
	// Exported to add hooks as GIT_HOP_TASK.
	Task string `json:"task,omitempty"`
}

// EnvHooks defines lifecycle hooks for an environment manager
type EnvHooks struct {
	PreStart  []string `json:"preStart,omitempty"`
	PostStart []string `json:"postStart,omitempty"`
	PreStop   []string `json:"preStop,omitempty"`
	PostStop  []string `json:"postStop,omitempty"`
}

// EnvConfig represents per-repo environment configuration
type EnvConfig struct {
	Hooks EnvHooks `json:"hooks,omitempty"`
}

type HubSettings struct {
	CompareBranch      *string    `json:"compareBranch,omitempty"`
	EnvPatterns        []string   `json:"envPatterns"`
	EnvironmentManager *string    `json:"environmentManager,omitempty"`
	EnvironmentConfig  *EnvConfig `json:"environmentConfig,omitempty"`
}

// HopspaceConfig represents $GIT_HOP_DATA_HOME/<org>/<repo>/hop.json
type HopspaceConfig struct {
	Repo            RepoConfig                        `json:"repo"`
	Branches        map[string]HopspaceBranch         `json:"branches"`
	Forks           map[string]HopspaceFork           `json:"forks"`
	PackageManagers map[string]PackageManagerOverride `json:"packageManagers,omitempty"` // Repo-level PM overrides
}

type HopspaceBranch struct {
	Exists          bool                              `json:"exists"`
	Path            string                            `json:"path"`
	LastSync        time.Time                         `json:"lastSync"`
	PackageManagers map[string]PackageManagerOverride `json:"packageManagers,omitempty"` // Branch-level PM overrides
}

// PackageManagerOverride allows overriding install commands at repo or branch level
type PackageManagerOverride struct {
	InstallCmd []string `json:"installCmd,omitempty"` // Override install command
}

type HopspaceFork struct {
	URI             string                            `json:"uri"`
	Org             string                            `json:"org"`
	Repo            string                            `json:"repo"`
	Branches        map[string]HopspaceBranch         `json:"branches"`
	PackageManagers map[string]PackageManagerOverride `json:"packageManagers,omitempty"` // Fork-level PM overrides
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
	// OverrideDir is the directory holding the worktree's compose
	// override. Empty in an entry an earlier release wrote, whose
	// override is in the repository-wide cache directory of the branch.
	OverrideDir string `json:"overrideDir,omitempty"`
	// Project is the compose project the worktree's environment runs as.
	// Empty in an entry an earlier release wrote, which runs as
	// <org>-<repo>-<branch>.
	Project string `json:"project,omitempty"`
	// Branch, Worktree and Hub say whose entry it is. An earlier release
	// wrote none of them; its entries are keyed by branch.
	Branch   string `json:"branch,omitempty"`
	Worktree string `json:"worktree,omitempty"`
	Hub      string `json:"hub,omitempty"`
}

// VolumesConfig represents volumes.json
type VolumesConfig struct {
	BasePath string                   `json:"basePath"`
	Branches map[string]BranchVolumes `json:"branches"`
}

type BranchVolumes struct {
	Volumes map[string]string `json:"volumes"`
}

// GlobalConfig holds global preferences: scalars come from git config
// hop.* keys, manager lists from managers.json. Its JSON shape is the legacy
// global.json format.
type GlobalConfig struct {
	Defaults            DefaultSettings          `json:"defaults"`
	ShellIntegration    ShellIntegrationSettings `json:"shellIntegration,omitempty"`
	PackageManagers     []PackageManagerConfig   `json:"packageManagers,omitempty"`
	EnvironmentManagers []EnvManagerConfig       `json:"environmentManagers,omitempty"`
	Backup              BackupSettings           `json:"backup,omitempty"`
}

// PackageManagerConfig represents a custom package manager configuration
type PackageManagerConfig struct {
	Name        string   `json:"name"`
	DetectFiles []string `json:"detectFiles"`
	LockFiles   []string `json:"lockFiles"`
	DepsDir     string   `json:"depsDir"`
	InstallCmd  []string `json:"installCmd"`
}

// EnvManagerConfig represents an environment manager configuration
type EnvManagerConfig struct {
	Name        string      `json:"name"`
	DetectFiles []string    `json:"detectFiles"`
	Commands    EnvCommands `json:"commands"`
	Hooks       EnvHooks    `json:"hooks,omitempty"`
}

// EnvCommands defines lifecycle commands for an environment manager
type EnvCommands struct {
	Start   []string `json:"start"`
	Stop    []string `json:"stop"`
	Health  []string `json:"health,omitempty"`
	Restart []string `json:"restart,omitempty"`
	Logs    []string `json:"logs,omitempty"`
}

// DefaultSettings represents global default settings
type DefaultSettings struct {
	EnvAutoStart     bool   `json:"envAutoStart"`
	GitDomain        string `json:"gitDomain"`
	WorktreeLocation string `json:"worktreeLocation,omitempty"`
	// DefaultStartPoint controls the start-point used by `git hop add` when
	// creating a new branch. Allowed values: "default-branch" (default — tip
	// of repo.defaultBranch), "initial" (root commit, legacy behavior), or
	// any explicit refspec (branch name, "origin/<name>", tag, SHA).
	DefaultStartPoint string `json:"defaultStartPoint,omitempty"`
	// HooksInstallMode controls how committed .git-hop/hooks/ scripts are
	// mirrored into the user's hopspace on clone/init. Allowed values:
	// "prompt" (default in interactive TTY), "symlink", "copy", "none".
	HooksInstallMode string `json:"hooksInstallMode,omitempty"`
}

// ShellIntegrationSettings tracks shell wrapper installation status
type ShellIntegrationSettings struct {
	Status         string    `json:"status"`                   // unknown, approved, declined, disabled
	InstalledShell string    `json:"installedShell,omitempty"` // bash, zsh, fish
	InstalledPath  string    `json:"installedPath,omitempty"`  // path to rc file
	InstalledAt    time.Time `json:"installedAt,omitempty"`
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
	Hints        []string        `json:"hints,omitempty"`
	Metadata     *BackupMetadata `json:"metadata,omitempty"`
	CreatedFiles []string        `json:"createdFiles"`
	ModifiedDirs []string        `json:"modifiedDirs"`
	// Carried lists the linked worktrees a bare conversion carried into
	// the hub.
	Carried []CarriedWorktree `json:"carried,omitempty"`
}

// CarriedWorktree is a linked worktree a bare conversion carried into the
// hub.
type CarriedWorktree struct {
	// Path is where the worktree is after the conversion.
	Path string `json:"path"`
	// Branch is its checked-out branch; empty when HEAD is detached.
	Branch string `json:"branch,omitempty"`
	// MovedFrom is its path before the conversion when it moved: a
	// worktree inside the repository's working tree moves into hops/.
	MovedFrom string `json:"movedFrom,omitempty"`
}

type BackupSettings struct {
	KeepBackup     bool `json:"keepBackup"`
	MaxBackups     int  `json:"maxBackups"`
	CleanupAgeDays int  `json:"cleanupAgeDays"`
}

// ResolveWorktreePath resolves a worktree path that may be relative or absolute.
// If the path is relative, it resolves it relative to the hub path.
// If the path is absolute, it returns it as-is.
func ResolveWorktreePath(worktreePath, hubPath string) string {
	if filepath.IsAbs(worktreePath) {
		return worktreePath
	}
	return filepath.Join(hubPath, worktreePath)
}

// MakeWorktreePath creates the standard worktree path pattern for hub configs.
// Returns "hops/{branchName}" as a relative path.
func MakeWorktreePath(branchName string) string {
	return filepath.Join("hops", branchName)
}
