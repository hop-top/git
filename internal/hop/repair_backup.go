package hop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/afero"
)

// repairBackupVersion is bumped when the on-disk manifest schema changes.
const repairBackupVersion = 1

// repairBackupPrefix is the dir-name prefix used for every repair backup
// inside <hub>/.hop/backups/. List/GC code filters by this prefix.
const repairBackupPrefix = "repair-"

// repairBackupTimeFormat is the UTC stamp embedded in backup directory
// names. Compact, lexicographically sortable, second-resolution.
const repairBackupTimeFormat = "20060102T150405Z"

// RepairManifest is the on-disk metadata for a single repair backup.
//
// Files maps a logical key (".git/worktrees", "hop.json", or
// "worktree:<path>/.git") to its sha256 hex digest at backup time. The
// applier and undo path use Files to verify byte-identity on restore.
type RepairManifest struct {
	Version    int               `json:"version"`
	ID         string            `json:"id"`
	Timestamp  time.Time         `json:"timestamp"`
	HubPath    string            `json:"hubPath"`
	Actions    []Action          `json:"actions"`
	Files      map[string]string `json:"files"`
}

// RepairBackup creates per-repair snapshots and exposes them for undo.
//
// Backup root: <hub>/.hop/backups/. Each snapshot lives in a sibling
// directory `repair-<UTC-timestamp>/` containing:
//
//   .git_worktrees/          copy of <hub>/.git/worktrees (full subtree)
//   hop.json                 copy of <hub>/hop.json
//   pointers/<wtBase>.gitptr  copy of each affected worktree's .git pointer file
//   manifest.json            RepairManifest
type RepairBackup struct {
	fs      afero.Fs
	hubPath string
}

// NewRepairBackup constructs a RepairBackup for the given hub.
func NewRepairBackup(fs afero.Fs, hubPath string) *RepairBackup {
	return &RepairBackup{fs: fs, hubPath: hubPath}
}

// Snapshot creates a new backup directory based on plan and returns the
// generated manifest. Pure file ops + sha256 — no git involved.
//
// affected is the set of worktree paths whose .git pointer files should
// also be captured; Snapshot derives this from plan.Actions itself.
func (b *RepairBackup) Snapshot(plan *Plan) (*RepairManifest, error) {
	id := repairBackupPrefix + time.Now().UTC().Format(repairBackupTimeFormat)
	dir := filepath.Join(b.hubPath, ".hop", "backups", id)
	if err := b.fs.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir backup: %w", err)
	}

	manifest := &RepairManifest{
		Version:   repairBackupVersion,
		ID:        id,
		Timestamp: time.Now().UTC(),
		HubPath:   b.hubPath,
		Actions:   append([]Action(nil), plan.Actions...),
		Files:     map[string]string{},
	}

	// Copy .git/worktrees if present.
	srcGW := filepath.Join(b.hubPath, ".git", "worktrees")
	dstGW := filepath.Join(dir, ".git_worktrees")
	if exists, _ := afero.DirExists(b.fs, srcGW); exists {
		sum, err := copyTreeWithSum(b.fs, srcGW, dstGW)
		if err != nil {
			return nil, fmt.Errorf("snapshot .git/worktrees: %w", err)
		}
		manifest.Files[".git/worktrees"] = sum
	}

	// Copy hop.json.
	srcHJ := filepath.Join(b.hubPath, "hop.json")
	if data, err := afero.ReadFile(b.fs, srcHJ); err == nil {
		if err := afero.WriteFile(b.fs, filepath.Join(dir, "hop.json"), data, 0644); err != nil {
			return nil, fmt.Errorf("snapshot hop.json: %w", err)
		}
		manifest.Files["hop.json"] = sha256Hex(data)
	}

	// Copy each affected worktree's .git pointer (file-shape only).
	pointersDir := filepath.Join(dir, "pointers")
	if err := b.fs.MkdirAll(pointersDir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir pointers: %w", err)
	}
	for _, a := range plan.Actions {
		if a.Kind == ActionNoOp {
			continue
		}
		gitPtr := filepath.Join(a.WorktreePath, ".git")
		info, err := b.fs.Stat(gitPtr)
		if err != nil || info.IsDir() {
			continue
		}
		data, err := afero.ReadFile(b.fs, gitPtr)
		if err != nil {
			continue
		}
		key := pointerKey(a.WorktreePath)
		dstPtr := filepath.Join(pointersDir, key)
		if err := afero.WriteFile(b.fs, dstPtr, data, 0644); err != nil {
			return nil, fmt.Errorf("snapshot pointer for %s: %w", a.WorktreePath, err)
		}
		manifest.Files["worktree:"+a.WorktreePath+"/.git"] = sha256Hex(data)
	}

	if err := writeManifest(b.fs, dir, manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

// List returns all backups for this hub, newest first. Empty slice when
// no backups exist (not an error).
func (b *RepairBackup) List() ([]RepairManifest, error) {
	root := filepath.Join(b.hubPath, ".hop", "backups")
	exists, err := afero.DirExists(b.fs, root)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	entries, err := afero.ReadDir(b.fs, root)
	if err != nil {
		return nil, err
	}
	var out []RepairManifest
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), repairBackupPrefix) {
			continue
		}
		m, err := readManifest(b.fs, filepath.Join(root, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp.After(out[j].Timestamp)
	})
	return out, nil
}

// Path returns the absolute path of the backup directory for id.
func (b *RepairBackup) Path(id string) string {
	return filepath.Join(b.hubPath, ".hop", "backups", id)
}

// Latest returns the most recent backup, or (nil, nil) when none exist.
func (b *RepairBackup) Latest() (*RepairManifest, error) {
	list, err := b.List()
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}
	return &list[0], nil
}

// pointerKey mangles an absolute worktree path into a flat filename safe
// for the pointers/ subdirectory. Slashes become underscores; the result
// is unique per worktree path.
func pointerKey(absPath string) string {
	cleaned := strings.TrimPrefix(filepath.Clean(absPath), string(filepath.Separator))
	cleaned = strings.ReplaceAll(cleaned, string(filepath.Separator), "_")
	return cleaned + ".gitptr"
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// copyTreeWithSum recursively copies src into dst and returns a single
// sha256 over the concatenation of (relative-path,nul,content,nul) for
// every regular file, in lexicographic order. The digest serves as the
// tree's identity for byte-equality checks during undo verification.
func copyTreeWithSum(fs afero.Fs, src, dst string) (string, error) {
	if err := fs.MkdirAll(dst, 0755); err != nil {
		return "", err
	}
	hasher := sha256.New()
	files, err := walkRegularFiles(fs, src)
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	for _, rel := range files {
		srcF := filepath.Join(src, rel)
		dstF := filepath.Join(dst, rel)
		if err := fs.MkdirAll(filepath.Dir(dstF), 0755); err != nil {
			return "", err
		}
		data, err := afero.ReadFile(fs, srcF)
		if err != nil {
			return "", err
		}
		info, err := fs.Stat(srcF)
		if err != nil {
			return "", err
		}
		if err := afero.WriteFile(fs, dstF, data, info.Mode().Perm()); err != nil {
			return "", err
		}
		_, _ = hasher.Write([]byte(rel))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write(data)
		_, _ = hasher.Write([]byte{0})
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// walkRegularFiles returns relative paths of all regular files under root.
// afero.Walk wraps filepath.Walk which calls os.Lstat — for MemMapFs we
// emulate it via afero.Walk against the MemMapFs.
func walkRegularFiles(fs afero.Fs, root string) ([]string, error) {
	var out []string
	walker := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, rel)
		return nil
	}
	if err := afero.Walk(fs, root, walker); err != nil {
		return nil, err
	}
	return out, nil
}

func writeManifest(fs afero.Fs, dir string, m *RepairManifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	return afero.WriteFile(fs, filepath.Join(dir, "manifest.json"), data, 0644)
}

func readManifest(fs afero.Fs, dir string) (*RepairManifest, error) {
	data, err := afero.ReadFile(fs, filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var m RepairManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}
