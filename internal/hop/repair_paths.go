package hop

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// Repair keeps its lock and its backups OUTSIDE the hub.
//
// The hub is the user's directory: in the bare-hub layout it is the git
// dir itself plus hop.json and hops/<branch>/. Earlier releases wrote
// <hub>/.hop/repair.lock and <hub>/.hop/backups/, which conjured a .hop/
// directory in hubs that never had one. Other tools treat the mere
// presence of <hub>/.hop/ as their config root, so a repair silently
// re-routed them. .hop/ is not git-hop's directory to create.
//
// The new home is the XDG state dir, keyed by hub:
//
//	$XDG_STATE_HOME/git-hop/repair/<hub-basename>-<hash>/
//	    repair.lock              held for the duration of a run, unlinked after
//	    backups/repair-<ts>Z/    one snapshot per mutating run
//
// Keyed by hub path rather than by repository on purpose: repair exists to
// fix a damaged hop.json, and the repository identity (org/repo) lives in
// that same file. The hub path is the one input a repair always has.
// State, not data: these are undo snapshots and a lock, which is what the
// XDG state dir is for; state.json already lives there.

// repairStateSubdir is the directory under the git-hop state dir that
// holds one entry per hub.
const repairStateSubdir = "repair"

// legacyRepairDirName is the hub-local directory earlier releases used.
// Read for backward compatibility, never written.
const legacyRepairDirName = ".hop"

// RepairStateDir returns the per-hub directory that holds repair's lock
// and backups. Stable across path spelling (trailing slash, dot segments)
// and, when the hub exists on disk, across symlinked spellings too.
func RepairStateDir(hubPath string) string {
	return filepath.Join(stateDir(), repairStateSubdir, hubKey(hubPath))
}

// RepairLockPath returns the lock file guarding concurrent repairs of hub.
func RepairLockPath(hubPath string) string {
	return filepath.Join(RepairStateDir(hubPath), "repair.lock")
}

// RepairBackupRoot returns the directory new repair backups are written to.
func RepairBackupRoot(hubPath string) string {
	return filepath.Join(RepairStateDir(hubPath), "backups")
}

// LegacyRepairDir returns <hub>/.hop, where earlier releases kept the lock
// and backups. Only consulted for reading and for cleanup.
func LegacyRepairDir(hubPath string) string {
	return filepath.Join(hubPath, legacyRepairDirName)
}

// LegacyRepairBackupRoot returns <hub>/.hop/backups.
func LegacyRepairBackupRoot(hubPath string) string {
	return filepath.Join(LegacyRepairDir(hubPath), "backups")
}

// LegacyRepairLockPath returns <hub>/.hop/repair.lock.
func LegacyRepairLockPath(hubPath string) string {
	return filepath.Join(LegacyRepairDir(hubPath), "repair.lock")
}

// RepairBackupRoots lists every directory that may hold repair backups
// for hub, write location first. List/undo/GC read all of them; Snapshot
// writes only the first.
func RepairBackupRoots(hubPath string) []string {
	return []string{RepairBackupRoot(hubPath), LegacyRepairBackupRoot(hubPath)}
}

// hubKey derives a filesystem-safe, human-scannable directory name for a
// hub: its basename (sanitised) plus a short hash of its canonical path.
// The basename lets an operator find their hub in the state dir; the hash
// keeps two hubs with the same basename apart.
func hubKey(hubPath string) string {
	canon := canonicalHubPath(hubPath)
	sum := sha256.Sum256([]byte(canon))
	return sanitizeName(filepath.Base(canon)) + "-" + hex.EncodeToString(sum[:])[:12]
}

// canonicalHubPath cleans hubPath and, when the path exists on disk,
// resolves symlinks so a hub reached through a symlinked parent keys the
// same as one reached directly. Missing paths (tests, deleted hubs) fall
// back to the cleaned spelling.
func canonicalHubPath(hubPath string) string {
	cleaned := filepath.Clean(hubPath)
	if abs, err := filepath.Abs(cleaned); err == nil {
		cleaned = abs
	}
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		return resolved
	}
	return cleaned
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "hub"
	}
	return b.String()
}
