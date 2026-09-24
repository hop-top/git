package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Migration from the branch-keyed format (1.x) to worktrees keyed by
// path. The inputs in testdata/migrate are shaped like real state files:
// one hub; two hubs of one repository after the old format let the
// second hub's main overwrite the first's; a partially recorded
// repository (worktrees without a hub, a repository without hubs, a hub
// without worktrees); and a 2.x file an old release then added
// branch-keyed entries to. Each has a reviewed golden of the saved result.

var migrateFixtures = []string{"one_hub", "two_hubs", "partial", "mixed"}

// lastUpdatedLine matches the one field SaveState stamps with the time.
var lastUpdatedLine = regexp.MustCompile(`"lastUpdated": "[^"]*"`)

func normalizeSaved(data []byte) string {
	return lastUpdatedLine.ReplaceAllString(string(data), `"lastUpdated": "-"`)
}

// seedStateFile writes data as state.json on a fresh filesystem.
func seedStateFile(t *testing.T, data []byte) afero.Fs {
	t.Helper()
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll(GetStateHome(), 0o755))
	require.NoError(t, afero.WriteFile(fs, statePath(), data, 0o644))
	return fs
}

// loadAndSave loads state.json and saves it again, returning the bytes
// written.
func loadAndSave(t *testing.T, fs afero.Fs) []byte {
	t.Helper()
	st, err := LoadState(fs)
	require.NoError(t, err)
	require.NoError(t, SaveState(fs, st))
	data, err := afero.ReadFile(fs, statePath())
	require.NoError(t, err)
	return data
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "migrate", name+".json"))
	require.NoError(t, err)
	return data
}

func TestMigrate_Golden(t *testing.T) {
	for _, name := range migrateFixtures {
		t.Run(name, func(t *testing.T) {
			fs := seedStateFile(t, readFixture(t, name))
			got := normalizeSaved(loadAndSave(t, fs))

			golden := filepath.Join("testdata", "migrate", name+".golden")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				require.NoError(t, os.WriteFile(golden, []byte(got), 0o644))
			}
			want, err := os.ReadFile(golden)
			require.NoError(t, err, "golden missing (UPDATE_GOLDEN=1 writes it)")
			assert.Equal(t, string(want), got)
		})
	}
}

// v1 is the shape of the file before 2.0, frozen here: what an old
// release decodes into.
type v1State struct {
	Version      string             `json:"version"`
	Repositories map[string]*v1Repo `json:"repositories"`
	Orphaned     []*OrphanedEntry   `json:"orphaned"`
}

type v1Repo struct {
	URI           string                 `json:"uri"`
	Org           string                 `json:"org"`
	Repo          string                 `json:"repo"`
	DefaultBranch string                 `json:"defaultBranch"`
	Worktrees     map[string]*v1Worktree `json:"worktrees"`
	Hubs          []*HubState            `json:"hubs"`
}

type v1Worktree struct {
	Path         string    `json:"path"`
	Type         string    `json:"type"`
	HubPath      string    `json:"hubPath"`
	CreatedAt    time.Time `json:"createdAt"`
	LastAccessed time.Time `json:"lastAccessed"`
}

type entryRef struct {
	repo, key string
	wt        *WorktreeState
}

func entriesOf(st *State) []entryRef {
	var out []entryRef
	for id, repo := range st.Repositories {
		for key, wt := range repo.Worktrees {
			if wt != nil {
				out = append(out, entryRef{id, key, wt})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].repo+out[i].key < out[j].repo+out[j].key })
	return out
}

// No entry is lost: every input entry has an output entry for the same
// worktree (the same path, resolved) in the same repository, carrying the
// old key as its branch when it had none, and its type, hub and times
// unless a duplicate of the same worktree was merged into it. The output
// holds one entry per worktree; repositories, hubs and orphans are
// untouched.
func TestMigrate_LosesNoEntry(t *testing.T) {
	for _, name := range migrateFixtures {
		t.Run(name, func(t *testing.T) {
			in, err := parseState(readFixture(t, name))
			require.NoError(t, err)
			fs := seedStateFile(t, readFixture(t, name))
			out, err := LoadState(fs)
			require.NoError(t, err)

			perWorktree := map[string]int{} // repo + resolved path (or legacy key) -> input entries
			id := func(repo, key string, wt *WorktreeState) string {
				if wt.Path == "" {
					return repo + "|" + legacyKeyPrefix + key
				}
				return repo + "|" + ResolvePath(wt.Path)
			}
			for _, e := range entriesOf(in) {
				perWorktree[id(e.repo, e.key, e.wt)]++
			}
			assert.Len(t, entriesOf(out), len(perWorktree), "one output entry per worktree")

			for _, e := range entriesOf(in) {
				repo := out.Repositories[e.repo]
				require.NotNil(t, repo, "repository %s kept", e.repo)
				var got *WorktreeState
				if e.wt.Path == "" {
					got = repo.Worktrees[legacyKeyPrefix+e.key]
				} else {
					_, got, _ = repo.WorktreeAt(e.wt.Path)
				}
				require.NotNil(t, got, "%s %s (%s) kept", e.repo, e.key, e.wt.Path)

				wantBranch := e.wt.Branch
				if wantBranch == "" {
					wantBranch = e.key
				}
				merged := perWorktree[id(e.repo, e.key, e.wt)] > 1
				if !merged {
					assert.Equal(t, wantBranch, got.Branch)
					assert.Equal(t, e.wt.Path, got.Path)
					assert.Equal(t, e.wt.Type, got.Type)
					if e.wt.HubPath != "" {
						assert.Equal(t, e.wt.HubPath, got.HubPath)
					}
					assert.True(t, e.wt.CreatedAt.Equal(got.CreatedAt))
					assert.True(t, e.wt.LastAccessed.Equal(got.LastAccessed))
					continue
				}
				assert.False(t, got.CreatedAt.After(e.wt.CreatedAt), "merged: earliest creation")
				assert.False(t, got.LastAccessed.Before(e.wt.LastAccessed), "merged: latest access")
				assert.NotEmpty(t, got.Branch)
				if e.wt.Type != "" {
					assert.NotEmpty(t, got.Type)
				}
			}

			for idRepo, repo := range in.Repositories {
				assert.Equal(t, repo.Hubs, out.Repositories[idRepo].Hubs, "hubs untouched")
				assert.Equal(t, repo.URI, out.Repositories[idRepo].URI)
				assert.Equal(t, repo.DefaultBranch, out.Repositories[idRepo].DefaultBranch)
			}
			assert.Equal(t, len(in.Repositories), len(out.Repositories))
			assert.Equal(t, in.Orphaned, out.Orphaned)
		})
	}
}

// Migrating a migrated state changes nothing, and a v2 file saved again
// is the same file (the save time aside).
func TestMigrate_Idempotent(t *testing.T) {
	for _, name := range migrateFixtures {
		t.Run(name, func(t *testing.T) {
			fs := seedStateFile(t, readFixture(t, name))
			first := loadAndSave(t, fs)
			second := loadAndSave(t, fs)
			assert.Equal(t, normalizeSaved(first), normalizeSaved(second))

			st, err := parseState(first)
			require.NoError(t, err)
			assert.False(t, migrate(st), "a migrated state has nothing left to migrate")
			assert.False(t, hasLegacyEntries(st))
		})
	}
}

func backups(t *testing.T, fs afero.Fs) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	entries, err := afero.ReadDir(fs, BackupDir())
	if err != nil {
		return out
	}
	for _, e := range entries {
		data, err := afero.ReadFile(fs, filepath.Join(BackupDir(), e.Name()))
		require.NoError(t, err)
		out[e.Name()] = data
	}
	return out
}

// The first save that replaces a file holding branch-keyed entries backs
// it up byte for byte; later saves of the migrated file take no backup.
func TestSaveState_BacksUpLegacyFileOnce(t *testing.T) {
	for _, name := range migrateFixtures {
		t.Run(name, func(t *testing.T) {
			original := readFixture(t, name)
			fs := seedStateFile(t, original)

			loadAndSave(t, fs)
			got := backups(t, fs)
			require.Len(t, got, 1, "one backup of the legacy file")
			for file, data := range got {
				assert.True(t, IsBackupName(file), file)
				_, ok := BackupTime(file)
				assert.True(t, ok, "the name carries the time: %s", file)
				assert.Equal(t, string(original), string(data), "the backup is the file as it was")
			}

			loadAndSave(t, fs)
			assert.Len(t, backups(t, fs), 1, "a migrated file is not backed up again")
		})
	}
}

// A file without legacy entries is never backed up.
func TestSaveState_NoBackupForCurrentFormat(t *testing.T) {
	fs := afero.NewMemMapFs()
	st := NewState()
	st.AddRepository("r", &RepositoryState{})
	require.NoError(t, st.PutWorktree("r", &WorktreeState{Path: "/w/a", Branch: "a"}))
	require.NoError(t, SaveState(fs, st))
	require.NoError(t, SaveState(fs, st))
	assert.Empty(t, backups(t, fs))
}

// A backup is written once: another backup taken in the same second gets
// its own name and leaves the first as it was.
func TestBackupLegacyState_WriteOnce(t *testing.T) {
	fs := afero.NewMemMapFs()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

	first, err := backupLegacyState(fs, []byte("first"), now)
	require.NoError(t, err)
	second, err := backupLegacyState(fs, []byte("second"), now)
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
	data, err := afero.ReadFile(fs, first)
	require.NoError(t, err)
	assert.Equal(t, "first", string(data))
	assert.Equal(t, filepath.Join(BackupDir(), "state-20260924T120000Z.json"), first)
	stamp, ok := BackupTime(filepath.Base(second))
	assert.True(t, ok)
	assert.True(t, stamp.Equal(now))
}

// A release that predates 2.0 can still read what this one writes:
// worktrees stays an object keyed by string, and the fields it knows keep
// their names. Otherwise such a release would treat the file as unreadable
// and, in add, start a new state over it.
func TestSavedState_DecodesAsV1(t *testing.T) {
	for _, name := range migrateFixtures {
		t.Run(name, func(t *testing.T) {
			fs := seedStateFile(t, readFixture(t, name))
			saved := loadAndSave(t, fs)
			current, err := parseState(saved)
			require.NoError(t, err)

			var old v1State
			require.NoError(t, json.Unmarshal(saved, &old))
			require.Len(t, old.Repositories, len(current.Repositories))
			for id, repo := range current.Repositories {
				require.Contains(t, old.Repositories, id)
				assert.Len(t, old.Repositories[id].Worktrees, len(repo.Worktrees))
				for key, wt := range repo.Worktrees {
					assert.Equal(t, wt.Path, old.Repositories[id].Worktrees[key].Path)
				}
			}
		})
	}
}

// A state file that cannot be parsed is never replaced, and neither is
// one a newer release wrote.
func TestSaveState_NeverReplacesUnreadableOrNewerFile(t *testing.T) {
	for name, content := range map[string]string{
		"corrupt": "{not json",
		"newer":   `{"version": "3.0.0", "repositories": {}, "orphaned": []}`,
	} {
		t.Run(name, func(t *testing.T) {
			fs := seedStateFile(t, []byte(content))

			err := SaveState(fs, NewState())

			assert.Error(t, err)
			data, readErr := afero.ReadFile(fs, statePath())
			require.NoError(t, readErr)
			assert.Equal(t, content, string(data))
			assert.Empty(t, backups(t, fs))
		})
	}
}

// A newer file is read as it is, not migrated into this release's format.
func TestLoadState_NewerFormatNotMigrated(t *testing.T) {
	fs := seedStateFile(t, []byte(`{"version": "3.1.0", "repositories": {"r": {"worktrees": {"x": {"path": "/p"}}}}}`))
	st, err := LoadState(fs)
	require.NoError(t, err)
	assert.Equal(t, "3.1.0", st.Version)
	assert.Contains(t, st.Repositories["r"].Worktrees, "x")
}

// Two spellings of one directory (a symlinked parent) are one worktree.
func TestMigrate_SymlinkedSpellingsMerge(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	real := filepath.Join(root, "real")
	require.NoError(t, os.MkdirAll(filepath.Join(real, "hops", "main"), 0o755))
	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(real, link))

	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	st := &State{Repositories: map[string]*RepositoryState{"r": {
		Hubs: []*HubState{{Path: real}},
		Worktrees: map[string]*WorktreeState{
			filepath.Join(real, "hops", "main"): {Path: filepath.Join(real, "hops", "main"), Branch: "main", Type: "bare", HubPath: real, CreatedAt: newer, LastAccessed: newer},
			"main":                              {Path: filepath.Join(link, "hops", "main"), Type: "bare", CreatedAt: older, LastAccessed: older},
		},
	}}}

	assert.True(t, migrate(st))
	wts := st.Repositories["r"].Worktrees
	require.Len(t, wts, 1)
	for _, wt := range wts {
		assert.Equal(t, "main", wt.Branch)
		assert.Equal(t, real, wt.HubPath)
		assert.True(t, wt.CreatedAt.Equal(older))
		assert.True(t, wt.LastAccessed.Equal(newer))
	}
}

func TestNewerThanSupported(t *testing.T) {
	for v, want := range map[string]bool{"": false, "1.0.0": false, "2.0.0": false, "2.9": false, "3.0.0": true, "10": true, "x": false} {
		assert.Equal(t, want, newerThanSupported(v), v)
	}
	assert.False(t, strings.HasPrefix(Version, "1."))
}
