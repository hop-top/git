package hop

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hop.top/git/internal/git"
	"github.com/spf13/afero"
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
	repoName := sanitizePath(b.org + "-" + b.repo)
	b.backupDir = filepath.Join(GetCacheHome(), "git-hop", repoName, timestamp)

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
		fmt.Printf("Warning: failed to export stashes: %v\n", err)
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

	fmt.Printf("Backup created: %s\n", b.backupDir)
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
			fmt.Printf("Warning: failed to load stashes for restoration: %v\n", err)
		} else if err := stashManager.ImportStashes(targetPath, stashes); err != nil {
			fmt.Printf("Warning: failed to restore stashes: %v\n", err)
		}
	}

	fmt.Printf("Restored from backup: %s\n", b.backupDir)
	return nil
}

func (b *BackupManager) Cleanup() error {
	if b.backupDir == "" {
		return fmt.Errorf("no backup to clean up")
	}

	if err := b.fs.RemoveAll(b.backupDir); err != nil {
		return fmt.Errorf("failed to remove backup directory: %w", err)
	}

	fmt.Printf("Backup cleaned up: %s\n", b.backupDir)
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
// executable-flagged file in the user's tree (T-0166 bug 2).
func (b *BackupManager) copyDir(src, dst string) error {
	srcInfo, err := b.fs.Stat(src)
	if err != nil {
		return err
	}

	if err := b.fs.MkdirAll(dst, srcInfo.Mode().Perm()); err != nil {
		return err
	}
	// MkdirAll does not chmod when the dir already exists; force parity.
	if err := b.fs.Chmod(dst, srcInfo.Mode().Perm()); err != nil {
		return err
	}

	entries, err := afero.ReadDir(b.fs, src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := b.copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := b.copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// copyFile copies src to dst, preserving the source's permission bits.
// WriteFile honours mode only when creating; an explicit Chmod after write
// keeps exec bits intact even on overwrite (T-0166).
func (b *BackupManager) copyFile(src, dst string) error {
	info, err := b.fs.Stat(src)
	if err != nil {
		return err
	}

	data, err := afero.ReadFile(b.fs, src)
	if err != nil {
		return err
	}

	if err := afero.WriteFile(b.fs, dst, data, info.Mode().Perm()); err != nil {
		return err
	}
	return b.fs.Chmod(dst, info.Mode().Perm())
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

func (b *BackupManager) detectStructure(path string) string {
	gitDir := filepath.Join(path, ".git")
	worktreesDir := filepath.Join(gitDir, "worktrees")

	_, err := b.fs.Stat(worktreesDir)
	if err == nil {
		if b.isWorktree(path) {
			return "worktree-child"
		}
		return "bare-worktree-root"
	}

	_, err = b.fs.Stat(gitDir)
	if err == nil {
		return "standard"
	}

	return "unknown"
}

func (b *BackupManager) isWorktree(path string) bool {
	gitFile := filepath.Join(path, ".git")
	info, err := b.fs.Stat(gitFile)
	if err != nil {
		return false
	}

	if info.Mode()&fs.ModeSymlink == 0 {
		content, err := afero.ReadFile(b.fs, gitFile)
		if err != nil {
			return false
		}
		return strings.Contains(string(content), "gitdir:")
	}

	return true
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

	parts := strings.Split(metadata.RemoteUrl, "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("could not parse org/repo from backup metadata")
	}

	org := parts[len(parts)-2]
	repo := strings.TrimSuffix(parts[len(parts)-1], ".git")

	return &BackupManager{
		fs:        fs,
		git:       g,
		backupDir: backupPath,
		metadata:  &metadata,
		org:       org,
		repo:      repo,
	}, nil
}

func GetCacheBackupPath(org, repo string) string {
	repoName := sanitizePath(org + "-" + repo)
	return filepath.Join(GetCacheHome(), "git-hop", repoName)
}

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
