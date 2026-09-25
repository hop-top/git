package state

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// state.json is rewritten by add, remove, merge, move, clone, init,
// fork, prune and doctor --fix. Two git-hop processes must not undo each
// other: an entry one removes stays removed, an entry one adds stays
// added, whatever the other had loaded before.

const raceRepo = "github.com/org/repo"

// isolateStateHome points the state home at a directory of this test's
// own, so tests on the real disk never share a state.json.
func isolateStateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", home)
	return filepath.Join(home, "git-hop")
}

func raceWorktreePath(branch string) string {
	return filepath.Join("/hub", "hops", branch)
}

// seedRaceState saves a state recording one repository with a worktree
// of each seeded branch.
func seedRaceState(t *testing.T, fs afero.Fs, seeded []string) {
	t.Helper()
	st := NewState()
	st.AddRepository(raceRepo, &RepositoryState{URI: "https://github.com/org/repo.git", Org: "org", Repo: "repo"})
	for _, b := range seeded {
		require.NoError(t, st.PutWorktree(raceRepo, &WorktreeState{Path: raceWorktreePath(b), Branch: b, Type: "linked", HubPath: "/hub"}))
	}
	require.NoError(t, SaveState(fs, st))
}

func addLikeGitHopAdd(fs afero.Fs, branch string) error {
	return Update(fs, func(st *State) error {
		return st.PutWorktree(raceRepo, &WorktreeState{Path: raceWorktreePath(branch), Branch: branch, Type: "linked", HubPath: "/hub"})
	})
}

func removeLikeGitHopRemove(fs afero.Fs, branch string) error {
	return Update(fs, func(st *State) error {
		return st.RemoveWorktreeAt(raceRepo, raceWorktreePath(branch))
	})
}

func recordedBranches(t *testing.T, fs afero.Fs) []string {
	t.Helper()
	st, err := LoadState(fs)
	require.NoError(t, err)
	var names []string
	for _, wt := range st.Repositories[raceRepo].Worktrees {
		names = append(names, wt.Branch)
	}
	sort.Strings(names)
	return names
}

func seededBranches(n int) []string {
	var seeded []string
	for i := 0; i < n; i++ {
		seeded = append(seeded, fmt.Sprintf("seed-%d", i))
	}
	return seeded
}

func expectedAdds(adders, perAdder int) []string {
	var want []string
	for a := 0; a < adders; a++ {
		for i := 0; i < perAdder; i++ {
			want = append(want, fmt.Sprintf("add-%d-%d", a, i))
		}
	}
	sort.Strings(want)
	return want
}

func TestUpdate_ConcurrentWritersLoseNothing(t *testing.T) {
	const adders, perAdder = 4, 15
	seeded := seededBranches(23)
	for _, tc := range []struct {
		name string
		fs   func(t *testing.T) afero.Fs
	}{
		{"memory", func(t *testing.T) afero.Fs { return afero.NewMemMapFs() }},
		{"disk", func(t *testing.T) afero.Fs { isolateStateHome(t); return afero.NewOsFs() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := tc.fs(t)
			seedRaceState(t, fs, seeded)

			var wg sync.WaitGroup
			errs := make(chan error, adders*perAdder+len(seeded))
			for a := 0; a < adders; a++ {
				wg.Add(1)
				go func(a int) {
					defer wg.Done()
					for i := 0; i < perAdder; i++ {
						errs <- addLikeGitHopAdd(fs, fmt.Sprintf("add-%d-%d", a, i))
					}
				}(a)
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				for _, b := range seeded {
					errs <- removeLikeGitHopRemove(fs, b)
				}
			}()
			wg.Wait()
			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}

			assert.Equal(t, expectedAdds(adders, perAdder), recordedBranches(t, fs))
		})
	}
}

// The incident, as separate processes: sequential removes in one, adds
// in two others, all against one state.json on disk.
func TestUpdate_ConcurrentProcessesLoseNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes")
	}
	const adders, perAdder = 2, 25
	seeded := seededBranches(23)
	home := isolateStateHome(t)
	fs := afero.NewOsFs()
	seedRaceState(t, fs, seeded)

	var cmds []*exec.Cmd
	start := func(env ...string) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestStateHelperProcess$", "-test.count=1")
		cmd.Env = append(os.Environ(), append([]string{"STATE_HELPER_HOME=" + filepath.Dir(home)}, env...)...)
		var out strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &out
		require.NoError(t, cmd.Start())
		t.Cleanup(func() {
			if t.Failed() {
				t.Logf("helper %v:\n%s", env, out.String())
			}
		})
		cmds = append(cmds, cmd)
	}
	for a := 0; a < adders; a++ {
		start("STATE_HELPER_ADD="+strconv.Itoa(a), "STATE_HELPER_N="+strconv.Itoa(perAdder))
	}
	start("STATE_HELPER_REMOVE=" + strings.Join(seeded, ","))
	for _, cmd := range cmds {
		require.NoError(t, cmd.Wait())
	}

	assert.Equal(t, expectedAdds(adders, perAdder), recordedBranches(t, fs))
	leftovers, err := filepath.Glob(filepath.Join(home, "state.json*.tmp"))
	require.NoError(t, err)
	assert.Empty(t, leftovers, "no temp file is left behind")
	_, err = os.Stat(filepath.Join(home, LockName))
	assert.True(t, os.IsNotExist(err), "the lock file is gone once no one holds it: %v", err)
}

// TestStateHelperProcess is one git-hop process of
// TestUpdate_ConcurrentProcessesLoseNothing; it does nothing on its own.
func TestStateHelperProcess(t *testing.T) {
	home := os.Getenv("STATE_HELPER_HOME")
	if home == "" {
		t.Skip("helper process")
	}
	// The test binary's own TestMain moved the XDG homes; point the
	// state home back at the parent's.
	t.Setenv("XDG_STATE_HOME", home)
	fs := afero.NewOsFs()
	if list := os.Getenv("STATE_HELPER_REMOVE"); list != "" {
		for _, b := range strings.Split(list, ",") {
			require.NoError(t, removeLikeGitHopRemove(fs, b))
		}
		return
	}
	n, err := strconv.Atoi(os.Getenv("STATE_HELPER_N"))
	require.NoError(t, err)
	for i := 0; i < n; i++ {
		require.NoError(t, addLikeGitHopAdd(fs, fmt.Sprintf("add-%s-%d", os.Getenv("STATE_HELPER_ADD"), i)))
	}
}

// Saves that overlap each write a temp file of their own: none writes
// into or renames away another's copy, so every one succeeds and
// state.json is always one of them whole. replaceFile is called without
// the lock on purpose, as by a writer that bypassed it.
func TestReplaceFile_OverlappingSavesDoNotCollide(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	fs := afero.NewOsFs()

	const writers, rounds = 8, 40
	payload := func(w int) []byte {
		data, err := json.Marshal(map[string]string{"writer": strconv.Itoa(w), "pad": strings.Repeat("x", 64<<10)})
		require.NoError(t, err)
		return data
	}
	payloads := make([][]byte, writers)
	for w := range payloads {
		payloads[w] = payload(w)
	}

	var wg sync.WaitGroup
	errs := make(chan error, writers*rounds)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				errs <- replaceFile(fs, path, payloads[w])
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	var decoded map[string]string
	require.NoError(t, json.Unmarshal(got, &decoded), "state.json is one save whole")
	w, err := strconv.Atoi(decoded["writer"])
	require.NoError(t, err)
	assert.Equal(t, string(payloads[w]), string(got))

	leftovers, err := filepath.Glob(filepath.Join(dir, "state.json*.tmp"))
	require.NoError(t, err)
	assert.Empty(t, leftovers, "no temp file is left behind")

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "state.json keeps its mode")
}

// Runs that load a file needing migration at once back it up once: the
// first save backs it up and migrates it under the lock, and every later
// save, also under the lock, finds it migrated. The runs wait for one
// another after loading (up to a bound: under Update only one is ever
// inside the callback), so without the lock every one would load, and
// back up, the unmigrated file.
func TestUpdate_ConcurrentMigrationBacksUpOnce(t *testing.T) {
	const runs = 6
	writers := map[string]func(fs afero.Fs, loaded func()) error{
		"Update": func(fs afero.Fs, loaded func()) error {
			return Update(fs, func(*State) error { loaded(); return nil })
		},
		"SaveState": func(fs afero.Fs, loaded func()) error {
			st, err := LoadState(fs)
			if err != nil {
				return err
			}
			loaded()
			return SaveState(fs, st)
		},
	}
	for writer, save := range writers {
		for _, name := range migrateFixtures {
			t.Run(writer+"/"+name, func(t *testing.T) {
				isolateStateHome(t)
				fs := afero.NewOsFs()
				original := readFixture(t, name)
				require.NoError(t, os.MkdirAll(GetStateHome(), 0o755))
				require.NoError(t, os.WriteFile(statePath(), original, 0o644))

				var arrived sync.WaitGroup
				arrived.Add(runs)
				allLoaded := make(chan struct{})
				go func() { arrived.Wait(); close(allLoaded) }()
				loaded := func() {
					arrived.Done()
					select {
					case <-allLoaded:
					case <-time.After(20 * time.Millisecond):
					}
				}

				var wg sync.WaitGroup
				errs := make(chan error, runs)
				for i := 0; i < runs; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						errs <- save(fs, loaded)
					}()
				}
				wg.Wait()
				close(errs)
				for err := range errs {
					require.NoError(t, err)
				}

				got := backups(t, fs)
				require.Len(t, keys(got), 1, "one backup of the legacy file")
				for _, data := range got {
					assert.Equal(t, string(original), string(data), "the backup is the file as it was")
				}
			})
		}
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Update saves nothing when its callback fails or asks to skip the save.
func TestUpdate_CallbackErrorOrSkipSavesNothing(t *testing.T) {
	fs := afero.NewMemMapFs()
	seedRaceState(t, fs, []string{"kept"})
	before, err := afero.ReadFile(fs, statePath())
	require.NoError(t, err)

	boom := fmt.Errorf("boom")
	err = Update(fs, func(st *State) error {
		st.Repositories = nil
		return boom
	})
	assert.ErrorIs(t, err, boom)

	require.NoError(t, Update(fs, func(st *State) error {
		st.Repositories = nil
		return ErrSkipSave
	}))

	after, err := afero.ReadFile(fs, statePath())
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after))
}

// A live holder makes Update wait, and give up with ErrLocked rather
// than wait forever; nothing is saved.
func TestUpdate_HeldLockTimesOut(t *testing.T) {
	isolateStateHome(t)
	prev := lockTimeout
	lockTimeout = 100 * time.Millisecond
	t.Cleanup(func() { lockTimeout = prev })

	fs := afero.NewOsFs()
	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- withStateLock(fs, func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held

	ran := false
	err := Update(fs, func(*State) error { ran = true; return nil })
	close(release)
	require.NoError(t, <-done)
	assert.ErrorIs(t, err, ErrLocked)
	assert.False(t, ran, "the callback ran without the lock")
	_, statErr := os.Stat(statePath())
	assert.True(t, os.IsNotExist(statErr), "nothing saved: %v", statErr)
}

// Every state writer outside this package goes through Update: a
// SaveState of a state loaded earlier saves over whatever another run
// saved since. SaveState stays for tests seeding a state file.
func TestNoWriterSavesLoadedState(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	self, err := filepath.Abs(".")
	require.NoError(t, err)

	var offenders []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == self || (path != root && strings.HasPrefix(d.Name(), ".")) || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "state.SaveState(") {
			rel, _ := filepath.Rel(root, path)
			offenders = append(offenders, rel)
		}
		return nil
	})
	require.NoError(t, err)
	assert.Empty(t, offenders, "use state.Update to change state.json")
}
