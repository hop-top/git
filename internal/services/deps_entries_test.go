package services_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/services"
)

// A worktree's DepsDir is a real directory with one link per entry of the
// store install: emptying it removes links, never the install.

// writeInstall populates the store install for key the way npm lays one
// out: packages, a scope, .bin with a relative link, npm's hidden
// lockfile, and a tool cache.
func writeInstall(t *testing.T, hopspace, key string) string {
	t.Helper()
	install := filepath.Join(services.DepsStorePath(hopspace), key)
	files := map[string]string{
		"a/index.js":         "a\n",
		"a/cli.js":           "cli\n",
		"b/index.js":         "b\n",
		"@s/x/index.js":      "x\n",
		"@s/y/index.js":      "y\n",
		".package-lock.json": "lock\n",
		".cache/tool/data":   "cache\n",
	}
	for rel, content := range files {
		path := filepath.Join(install, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(install, ".bin"), 0o755))
	require.NoError(t, os.Symlink("../a/cli.js", filepath.Join(install, ".bin", "cli")))
	return install
}

// assertEntryLinked checks that worktree's node_modules is a real
// directory linked entry by entry into install.
func assertEntryLinked(t *testing.T, worktree, install string) {
	t.Helper()
	nm := filepath.Join(worktree, "node_modules")
	info, err := os.Lstat(nm)
	require.NoError(t, err)
	require.True(t, info.IsDir(), "node_modules must be a real directory")
	entries, err := os.ReadDir(install)
	require.NoError(t, err)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") || strings.HasPrefix(e.Name(), "@") {
			continue
		}
		target, err := os.Readlink(filepath.Join(nm, e.Name()))
		require.NoError(t, err, "%s must be a link", e.Name())
		assert.Equal(t, filepath.Join(install, e.Name()), target)
	}
	record, err := os.ReadFile(filepath.Join(nm, services.EntryLinksMarker))
	require.NoError(t, err)
	assert.Contains(t, string(record), `"install": "`+install+`"`)
}

type entryLinksFixture struct {
	hopspace, wt, key, install, nm string
	dm                             *services.DepsManager
}

// newEntryLinksFixture lays out worktree main's node_modules entry by
// entry for the install its lockfile names.
func newEntryLinksFixture(t *testing.T) entryLinksFixture {
	t.Helper()
	hopspace := setupDepsTestDir(t)
	wt, key := newWorktree(t, filepath.Join(hopspace, "hops", "main"), "lockfileVersion: 6\n")
	install := writeInstall(t, hopspace, key)
	dm := newStoreManager(t, hopspace, pnpmPM())
	nm := filepath.Join(wt, "node_modules")
	require.NoError(t, dm.LayEntryLinksForTest(install, nm))
	return entryLinksFixture{hopspace: hopspace, wt: wt, key: key, install: install, nm: nm, dm: dm}
}

func (f entryLinksFixture) audit(t *testing.T) []services.Issue {
	t.Helper()
	issues, err := f.dm.Audit(map[string]string{"main": f.wt})
	require.NoError(t, err)
	return issues
}

func TestDepsEntries_Layout(t *testing.T) {
	f := newEntryLinksFixture(t)

	info, err := os.Lstat(f.nm)
	require.NoError(t, err)
	assert.True(t, info.IsDir(), "node_modules is a real directory")
	for _, rel := range []string{"a", "b", "@s/x", "@s/y", ".bin/cli"} {
		target, err := os.Readlink(filepath.Join(f.nm, rel))
		require.NoError(t, err, "%s must be a link", rel)
		assert.Equal(t, filepath.Join(f.install, rel), target)
	}
	for _, dir := range []string{"@s", ".bin"} {
		info, err := os.Lstat(filepath.Join(f.nm, dir))
		require.NoError(t, err)
		assert.True(t, info.IsDir(), "%s is a real directory with a link per child", dir)
	}
	cli, err := os.ReadFile(filepath.Join(f.nm, ".bin", "cli"))
	require.NoError(t, err)
	assert.Equal(t, "cli\n", string(cli), ".bin commands resolve through the install")

	info, err = os.Lstat(filepath.Join(f.nm, ".package-lock.json"))
	require.NoError(t, err)
	assert.True(t, info.Mode().IsRegular(), "hidden files are copied, not linked")
	assert.NoFileExists(t, filepath.Join(f.nm, ".cache"), "tool caches are per worktree")
	assert.FileExists(t, filepath.Join(f.nm, services.EntryLinksMarker))
}

// Writing the copied hidden lockfile and emptying the DepsDir, as npm ci
// and rm -rf node_modules/* do, leave the install as it was.
func TestDepsEntries_EmptyingWorktreeLeavesInstall(t *testing.T) {
	f := newEntryLinksFixture(t)
	before := snapshotTree(t, f.install)

	require.NoError(t, os.WriteFile(filepath.Join(f.nm, ".package-lock.json"), []byte("rewritten\n"), 0o644))
	entries, err := os.ReadDir(f.nm)
	require.NoError(t, err)
	for _, e := range entries {
		require.NoError(t, os.RemoveAll(filepath.Join(f.nm, e.Name())))
	}
	require.NoError(t, os.RemoveAll(f.nm))

	assert.Equal(t, before, snapshotTree(t, f.install))
}

// A complete per-entry layout for the current install is set up; links
// outside the store (npm link) are the worktree's own.
func TestDepsEntries_AuditAcceptsLayout(t *testing.T) {
	f := newEntryLinksFixture(t)
	assert.Empty(t, f.audit(t))

	elsewhere := t.TempDir()
	require.NoError(t, os.Remove(filepath.Join(f.nm, "b")))
	require.NoError(t, os.Symlink(elsewhere, filepath.Join(f.nm, "b")))
	require.NoError(t, os.MkdirAll(filepath.Join(f.nm, ".vite"), 0o755))
	assert.Empty(t, f.audit(t))
}

// Missing and dangling links are damage, reported with the entries.
func TestDepsEntries_AuditMissingLinks(t *testing.T) {
	cases := map[string]struct {
		damage func(t *testing.T, f entryLinksFixture)
		want   []string
	}{
		"removed": {
			damage: func(t *testing.T, f entryLinksFixture) {
				require.NoError(t, os.Remove(filepath.Join(f.nm, "a")))
			},
			want: []string{"a"},
		},
		"removed in a scope": {
			damage: func(t *testing.T, f entryLinksFixture) {
				require.NoError(t, os.Remove(filepath.Join(f.nm, "@s", "y")))
			},
			want: []string{"@s/y"},
		},
		"bin removed": {
			damage: func(t *testing.T, f entryLinksFixture) {
				require.NoError(t, os.RemoveAll(filepath.Join(f.nm, ".bin")))
			},
			want: []string{".bin/cli"},
		},
		"dangling": {
			damage: func(t *testing.T, f entryLinksFixture) {
				require.NoError(t, os.Remove(filepath.Join(f.nm, "a")))
				require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "gone"), filepath.Join(f.nm, "a")))
			},
			want: []string{"a"},
		},
		"elsewhere in the store": {
			damage: func(t *testing.T, f entryLinksFixture) {
				require.NoError(t, os.Remove(filepath.Join(f.nm, "a")))
				require.NoError(t, os.Symlink(filepath.Join(f.install, "b"), filepath.Join(f.nm, "a")))
			},
			want: []string{"a"},
		},
		"emptied by a glob": {
			damage: func(t *testing.T, f entryLinksFixture) {
				entries, err := os.ReadDir(f.nm)
				require.NoError(t, err)
				for _, e := range entries {
					if !strings.HasPrefix(e.Name(), ".") {
						require.NoError(t, os.RemoveAll(filepath.Join(f.nm, e.Name())))
					}
				}
			},
			want: []string{"@s/x", "@s/y", "a", "b"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newEntryLinksFixture(t)
			tc.damage(t, f)
			issues := f.audit(t)
			require.Len(t, issues, 1)
			assert.Equal(t, services.IssueEntryLinks, issues[0].Type)
			assert.Equal(t, services.SeverityError, issues[0].Type.Severity())
			assert.Equal(t, tc.want, issues[0].Missing)
			assert.Equal(t, f.install, issues[0].SymlinkTarget)
			assert.Equal(t, f.key, issues[0].TargetName())
		})
	}
}

// npm install in a per-entry worktree replaces the links with package
// directories: a local folder.
func TestDepsEntries_AuditShadowedIsLocalFolder(t *testing.T) {
	f := newEntryLinksFixture(t)
	require.NoError(t, os.Remove(filepath.Join(f.nm, "a")))
	require.NoError(t, os.MkdirAll(filepath.Join(f.nm, "a"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(f.nm, "a", "index.js"), []byte("local\n"), 0o644))

	issues := f.audit(t)
	require.Len(t, issues, 1)
	assert.Equal(t, services.IssueLocalFolder, issues[0].Type)
	assert.Positive(t, issues[0].Size)
}

// Without its record, the layout is still read from its links.
func TestDepsEntries_AuditWithoutRecord(t *testing.T) {
	f := newEntryLinksFixture(t)
	require.NoError(t, os.Remove(filepath.Join(f.nm, services.EntryLinksMarker)))
	assert.Empty(t, f.audit(t))

	require.NoError(t, os.Remove(filepath.Join(f.nm, "b")))
	issues := f.audit(t)
	require.Len(t, issues, 1)
	assert.Equal(t, services.IssueEntryLinks, issues[0].Type)
}

func TestDepsEntries_AuditInstallGone(t *testing.T) {
	f := newEntryLinksFixture(t)
	require.NoError(t, os.RemoveAll(f.install))

	issues := f.audit(t)
	require.Len(t, issues, 1)
	assert.Equal(t, services.IssueBrokenSymlink, issues[0].Type)
	assert.Equal(t, f.install, issues[0].SymlinkTarget)
}

func TestDepsEntries_AuditStaleInstall(t *testing.T) {
	f := newEntryLinksFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(f.wt, "pnpm-lock.yaml"), []byte("lockfileVersion: 9\n"), 0o644))

	issues := f.audit(t)
	require.Len(t, issues, 1)
	assert.Equal(t, services.IssueStaleSymlink, issues[0].Type)
	assert.Equal(t, services.SeverityWarning, issues[0].Type.Severity())
	assert.Equal(t, f.install, issues[0].SymlinkTarget)
}

// gc keeps an install a worktree links into entry by entry, even for an
// older lockfile, even by one link in a scope with no record left.
func TestDepsEntries_GCKeepsInstallLinkedPerEntry(t *testing.T) {
	f := newEntryLinksFixture(t)
	require.NoError(t, os.WriteFile(filepath.Join(f.wt, "pnpm-lock.yaml"), []byte("lockfileVersion: 9\n"), 0o644))
	worktrees := map[string]string{"main": f.wt}
	f.dm.Registry.UpdateEntryMetadata(f.key, "", "pnpm-lock.yaml")

	orphaned, _, err := f.dm.GarbageCollect(worktrees, false)
	require.NoError(t, err)
	assert.NotContains(t, orphaned, f.key)
	assert.Equal(t, []string{"main"}, f.dm.Registry.Entries[f.key].UsedBy)

	require.NoError(t, os.Remove(filepath.Join(f.nm, services.EntryLinksMarker)))
	for _, rel := range []string{"a", "b", "@s/x", ".bin/cli"} {
		require.NoError(t, os.Remove(filepath.Join(f.nm, rel)))
	}
	orphaned, _, err = f.dm.GarbageCollect(worktrees, false)
	require.NoError(t, err)
	assert.NotContains(t, orphaned, f.key, "one link in a scope still uses it")
	assert.DirExists(t, f.install)

	require.NoError(t, os.RemoveAll(f.nm))
	orphaned, _, err = f.dm.GarbageCollect(worktrees, false)
	require.NoError(t, err)
	assert.Contains(t, orphaned, f.key)
	assert.NoDirExists(t, f.install)
}
