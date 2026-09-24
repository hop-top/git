package hop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/afero"
)

// conversionFailedMarker is written into a conversion backup when the
// conversion it guarded failed. Retention never removes such a backup: it
// is what the automatic rollback restored from, and the only copy of the
// repository if that rollback failed too.
const conversionFailedMarker = "conversion-failed"

// backupMetadataFile is written last by CreateBackup, so a backup
// directory without it is still being copied (or was abandoned mid-copy).
const backupMetadataFile = "backup-info.json"

// DefaultConversionBackupRoot is where conversion backups go when
// hop.backup.path is unset: the git-hop directory of the XDG cache home.
func DefaultConversionBackupRoot() string {
	return filepath.Join(GetCacheHome(), "git-hop")
}

// ResolveConversionBackupRoot validates a configured hop.backup.path
// value (already `~`-expanded by git's --type=path) and returns the root
// to use. Empty selects the default. A relative value is rejected: init
// runs inside the repository it converts and prune runs from anywhere,
// so a relative root would name a different directory each time, and
// during init one inside the tree being copied.
func ResolveConversionBackupRoot(configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return DefaultConversionBackupRoot(), nil
	}
	if !filepath.IsAbs(configured) {
		return "", fmt.Errorf("hop.backup.path %q is relative; use an absolute or ~/ path", configured)
	}
	return filepath.Clean(configured), nil
}

// ConversionBackupDir is the per-repository directory under root that
// holds that repository's timestamped conversion backups. An empty root
// means the default one.
func ConversionBackupDir(root, org, repo string) string {
	if root == "" {
		root = DefaultConversionBackupRoot()
	}
	return filepath.Join(root, sanitizePath(org+"-"+repo))
}

// ConversionBackupRoots returns the roots a repository's conversion
// backups may live under: the configured one first, then the default
// one, so backups taken before hop.backup.path changed stay reachable.
// Duplicates are dropped.
func ConversionBackupRoots(configured string) []string {
	roots := []string{}
	for _, r := range []string{configured, DefaultConversionBackupRoot()} {
		if r == "" {
			continue
		}
		r = filepath.Clean(r)
		dup := false
		for _, have := range roots {
			if have == r {
				dup = true
				break
			}
		}
		if !dup {
			roots = append(roots, r)
		}
	}
	return roots
}

// SetRoot points the manager at a conversion backup root other than the
// default one. It must be called before CreateBackup.
func (b *BackupManager) SetRoot(root string) {
	b.root = root
}

// MarkConversionFailed records in the backup that the conversion it
// guarded failed, so retention leaves it alone.
func (b *BackupManager) MarkConversionFailed(cause error) error {
	if b.backupDir == "" {
		return fmt.Errorf("no backup to mark")
	}
	msg := "conversion failed\n"
	if cause != nil {
		msg = fmt.Sprintf("conversion failed: %v\n", cause)
	}
	return afero.WriteFile(b.fs, filepath.Join(b.backupDir, conversionFailedMarker), []byte(msg), 0o644)
}

// ConversionBackup is one timestamped conversion backup directory.
type ConversionBackup struct {
	Path string
	// Time is when the backup was taken: the metadata timestamp, or the
	// directory's modification time when the metadata is unreadable.
	Time time.Time
	// OriginalPath is the repository path the backup copied, from the
	// metadata; empty when the metadata is missing or unreadable.
	OriginalPath string
	// Complete is false while the metadata file is missing: the copy is
	// still running or was abandoned part-way.
	Complete bool
	// Failed marks the backup of a conversion that failed.
	Failed bool
}

// ConversionBackupOwner names a repository whose conversion backups are
// wanted.
type ConversionBackupOwner struct {
	// Org and Repo name the <org>-<repo> directory the backups sit in.
	Org, Repo string
	// HubPaths are the repository's hubs. A backup whose recorded
	// original path is one of them belongs to the repository whatever
	// directory it sits in, so a hub whose org/repo changed after the
	// backup was named (a new remote, or the origin the bare clone
	// leaves) keeps its backups.
	HubPaths []string
}

// ListConversionBackups lists owner's conversion backups under every
// root, newest first. A root that does not exist contributes nothing.
func ListConversionBackups(fs afero.Fs, roots []string, owner ConversionBackupOwner) ([]ConversionBackup, error) {
	ownDir := sanitizePath(owner.Org + "-" + owner.Repo)
	hubs := make(map[string]bool, len(owner.HubPaths))
	for _, h := range owner.HubPaths {
		hubs[resolvePathForCompare(h)] = true
	}

	var backups []ConversionBackup
	for _, root := range roots {
		repoDirs, err := afero.ReadDir(fs, root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, repoDir := range repoDirs {
			if !repoDir.IsDir() {
				continue
			}
			named := owner.Org != "" && owner.Repo != "" && repoDir.Name() == ownDir
			if !named && len(hubs) == 0 {
				continue
			}
			dir := filepath.Join(root, repoDir.Name())
			entries, err := afero.ReadDir(fs, dir)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				b := readConversionBackup(fs, filepath.Join(dir, entry.Name()), entry.ModTime())
				if named || (b.OriginalPath != "" && hubs[resolvePathForCompare(b.OriginalPath)]) {
					backups = append(backups, b)
				}
			}
		}
	}
	sort.SliceStable(backups, func(i, j int) bool { return backups[i].Time.After(backups[j].Time) })
	return backups, nil
}

func readConversionBackup(fs afero.Fs, path string, modTime time.Time) ConversionBackup {
	b := ConversionBackup{Path: path, Time: modTime}
	if data, err := afero.ReadFile(fs, filepath.Join(path, backupMetadataFile)); err == nil {
		b.Complete = true
		var meta BackupMetadata
		if json.Unmarshal(data, &meta) == nil {
			if !meta.Timestamp.IsZero() {
				b.Time = meta.Timestamp
			}
			b.OriginalPath = meta.OriginalPath
		}
	}
	if exists, _ := afero.Exists(fs, filepath.Join(path, conversionFailedMarker)); exists {
		b.Failed = true
	}
	return b
}

// ConversionBackupRetention is the retention policy for one repository's
// conversion backups. A limit of zero or less is off.
type ConversionBackupRetention struct {
	// MaxBackups keeps at most this many of the newest backups.
	MaxBackups int
	// MaxAgeDays removes backups older than this many days.
	MaxAgeDays int
}

// ExpiredConversionBackups returns the backups retention removes: those
// beyond the newest MaxBackups, and those older than MaxAgeDays. Either
// limit alone is enough. Incomplete backups and backups of failed
// conversions are never returned, and do not count toward MaxBackups.
// backups must be sorted newest first, as ListConversionBackups returns
// them.
func ExpiredConversionBackups(backups []ConversionBackup, policy ConversionBackupRetention, now time.Time) []ConversionBackup {
	var cutoff time.Time
	if policy.MaxAgeDays > 0 {
		cutoff = now.Add(-time.Duration(policy.MaxAgeDays) * 24 * time.Hour)
	}
	var expired []ConversionBackup
	kept := 0
	for _, b := range backups {
		if !b.Complete || b.Failed {
			continue
		}
		overCount := policy.MaxBackups > 0 && kept >= policy.MaxBackups
		tooOld := !cutoff.IsZero() && b.Time.Before(cutoff)
		if overCount || tooOld {
			expired = append(expired, b)
			continue
		}
		kept++
	}
	return expired
}

// pathWithin reports whether path is dir or lies beneath it, comparing
// absolute, symlink-resolved forms so /tmp and /private/tmp agree.
func pathWithin(path, dir string) bool {
	p, d := resolvePathForCompare(path), resolvePathForCompare(dir)
	rel, err := filepath.Rel(d, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolvePathForCompare makes path absolute and resolves symlinks in its
// longest existing prefix; the part that does not exist yet is appended
// unchanged.
func resolvePathForCompare(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	rest := ""
	cur := abs
	for {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}
