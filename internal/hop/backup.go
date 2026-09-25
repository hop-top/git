package hop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/output"
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

type BackupManager struct {
	fs        afero.Fs
	git       git.GitInterface
	backupDir string
	metadata  *BackupMetadata
	org       string
	repo      string
	// root is the directory conversion backups are kept under; empty
	// means DefaultConversionBackupRoot().
	root string
}

func NewBackupManager(fs afero.Fs, g git.GitInterface, org, repo string) (*BackupManager, error) {
	if org == "" || repo == "" {
		return nil, fmt.Errorf("org and repo must be specified")
	}

	return &BackupManager{
		fs:   fs,
		git:  g,
		org:  org,
		repo: repo,
	}, nil
}

func (b *BackupManager) CreateBackup(repoPath string) error {
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	b.backupDir = filepath.Join(ConversionBackupDir(b.root, b.org, b.repo), timestamp)

	// A backup inside the tree it copies would copy itself, and a bare
	// conversion would then move it into the new worktree.
	if pathWithin(b.backupDir, repoPath) {
		return fmt.Errorf("backup location %s is inside the repository being converted (%s); set %s outside it",
			b.backupDir, repoPath, "hop.backup.path")
	}

	if err := b.fs.MkdirAll(b.backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	originalDir := filepath.Join(b.backupDir, "original")
	if err := b.copyDir(repoPath, originalDir); err != nil {
		return fmt.Errorf("failed to copy repository: %w", err)
	}

	gitStatus := b.getGitStatus(repoPath)
	currentBranch, _ := b.git.GetCurrentBranch(repoPath)
	remoteURL, _ := b.git.GetRemoteURL(repoPath)

	stashManager := NewStashManager(b.git, b.fs)
	stashes, err := stashManager.ExportStashes(repoPath)
	if err != nil {
		output.Warn("failed to export stashes: %v", err)
	}

	b.metadata = &BackupMetadata{
		Timestamp:     time.Now(),
		OriginalPath:  repoPath,
		RemoteUrl:     remoteURL,
		CurrentBranch: currentBranch,
		Structure:     b.detectStructure(repoPath),
		HasStashes:    len(stashes) > 0,
		StashCount:    len(stashes),
		GitStatus:     gitStatus,
	}

	if err := b.writeMetadata(stashes); err != nil {
		return fmt.Errorf("failed to write backup metadata: %w", err)
	}

	fmt.Fprintf(output.ReportOut(), "Backup created: %s\n", b.backupDir)
	return nil
}

func (b *BackupManager) Restore(targetPath string) error {
	if b.metadata == nil {
		if err := b.loadMetadata(); err != nil {
			return err
		}
	}

	originalDir := filepath.Join(b.backupDir, "original")
	exists, err := afero.DirExists(b.fs, originalDir)
	if err != nil {
		return fmt.Errorf("failed to check backup directory: %w", err)
	}
	if !exists {
		return fmt.Errorf("backup not found: %s", originalDir)
	}

	if exists, _ := afero.DirExists(b.fs, targetPath); exists {
		if err := b.fs.RemoveAll(targetPath); err != nil {
			return fmt.Errorf("failed to remove target directory: %w", err)
		}
	}

	targetParent := filepath.Dir(targetPath)
	if err := b.fs.MkdirAll(targetParent, 0755); err != nil {
		return fmt.Errorf("failed to create target parent directory: %w", err)
	}

	if err := b.copyDir(originalDir, targetPath); err != nil {
		return fmt.Errorf("failed to restore from backup: %w", err)
	}

	if b.metadata.HasStashes && b.metadata.StashCount > 0 {
		stashManager := NewStashManager(b.git, b.fs)
		stashes, err := b.loadStashes()
		if err != nil {
			output.Warn("failed to load stashes for restoration: %v", err)
		} else if err := stashManager.ImportStashes(targetPath, stashes); err != nil {
			output.Warn("failed to restore stashes: %v", err)
		}
	}

	fmt.Fprintf(output.ReportOut(), "Restored from backup: %s\n", b.backupDir)
	return nil
}

func (b *BackupManager) Cleanup() error {
	if b.backupDir == "" {
		return fmt.Errorf("no backup to clean up")
	}

	if err := b.fs.RemoveAll(b.backupDir); err != nil {
		return fmt.Errorf("failed to remove backup directory: %w", err)
	}

	fmt.Fprintf(output.ReportOut(), "Backup cleaned up: %s\n", b.backupDir)
	return nil
}

func (b *BackupManager) GetBackupPath() string {
	return b.backupDir
}

func (b *BackupManager) Exists() bool {
	if b.backupDir == "" {
		return false
	}
	exists, _ := afero.DirExists(b.fs, b.backupDir)
	return exists
}

// copyDir recursively copies src into dst, preserving permission bits on every
// directory and file. Mode preservation is critical for the restore-on-failure
// path: dropping exec bits silently corrupts .sh scripts and any other
// executable-flagged file in the user's tree.
func (b *BackupManager) copyDir(src, dst string) error {
	return copyTree(b.fs, src, dst)
}

func (b *BackupManager) writeMetadata(stashes []StashRef) error {
	metadataPath := filepath.Join(b.backupDir, "backup-info.json")
	data, err := json.MarshalIndent(b.metadata, "", "  ")
	if err != nil {
		return err
	}

	if err := afero.WriteFile(b.fs, metadataPath, data, 0644); err != nil {
		return err
	}

	if len(stashes) > 0 {
		stashPath := filepath.Join(b.backupDir, "stash-refs.json")
		stashData, err := json.MarshalIndent(stashes, "", "  ")
		if err != nil {
			return err
		}

		if err := afero.WriteFile(b.fs, stashPath, stashData, 0644); err != nil {
			return err
		}
	}

	return nil
}

func (b *BackupManager) loadMetadata() error {
	metadataPath := filepath.Join(b.backupDir, "backup-info.json")
	data, err := afero.ReadFile(b.fs, metadataPath)
	if err != nil {
		return fmt.Errorf("failed to read backup metadata: %w", err)
	}

	if err := json.Unmarshal(data, &b.metadata); err != nil {
		return fmt.Errorf("failed to parse backup metadata: %w", err)
	}

	return nil
}

func (b *BackupManager) loadStashes() ([]StashRef, error) {
	stashPath := filepath.Join(b.backupDir, "stash-refs.json")
	data, err := afero.ReadFile(b.fs, stashPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read stash references: %w", err)
	}

	var stashes []StashRef
	if err := json.Unmarshal(data, &stashes); err != nil {
		return nil, fmt.Errorf("failed to parse stash references: %w", err)
	}

	return stashes, nil
}

func (b *BackupManager) getGitStatus(path string) string {
	status, err := b.git.RunInDir(path, "git", "status", "--porcelain")
	if err != nil {
		return "unknown"
	}

	if status == "" {
		return "clean"
	}

	return "dirty"
}

// detectStructure labels the backed-up repository's layout for the
// backup metadata, using the same classification as init.
func (b *BackupManager) detectStructure(path string) string {
	switch DetectRepoStructure(b.fs, b.git, path) {
	case config.StandardRepo:
		return "standard"
	case config.BareWorktreeRoot:
		return "bare-worktree-root"
	case config.WorktreeRoot:
		return "worktree-root"
	case config.WorktreeChild:
		return "worktree-child"
	default:
		return "unknown"
	}
}

func sanitizePath(path string) string {
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", "-",
		" ", "_",
		"\t", "_",
	)
	return replacer.Replace(path)
}

func LoadBackupManager(fs afero.Fs, g git.GitInterface, backupPath string) (*BackupManager, error) {
	metadataPath := filepath.Join(backupPath, "backup-info.json")
	data, err := afero.ReadFile(fs, metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup metadata: %w", err)
	}

	var metadata BackupMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse backup metadata: %w", err)
	}

	// org/repo only name the directory a new backup is created in; a
	// loaded backup already has its directory, so a repository without a
	// remote (empty RemoteUrl) loads like any other.
	return &BackupManager{
		fs:        fs,
		git:       g,
		backupDir: backupPath,
		metadata:  &metadata,
	}, nil
}

// GetCacheBackupPath is the per-repository conversion backup directory
// under the default root.
func GetCacheBackupPath(org, repo string) string {
	return ConversionBackupDir("", org, repo)
}

// ListBackups lists the conversion backup directories of org/repo under
// the default root.
func ListBackups(fs afero.Fs, org, repo string) ([]string, error) {
	backupBase := GetCacheBackupPath(org, repo)
	entries, err := afero.ReadDir(fs, backupBase)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var backups []string
	for _, entry := range entries {
		if entry.IsDir() {
			backups = append(backups, filepath.Join(backupBase, entry.Name()))
		}
	}

	return backups, nil
}
