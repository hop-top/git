package services

import (
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

	"hop.top/git/internal/config"
	"hop.top/git/internal/docker"
	"hop.top/git/internal/filelock"
	"hop.top/git/internal/state"
)

// ports.json and volumes.json are rewritten by env generate (and add,
// clone, init), remove, remove --hub, move and prune. Runs working at
// once must not undo each other, and two allocations must never hand out
// one port: allocation reads every hub's records, so it has to see what
// every other run recorded.

// raceCompose is the compose config the helper runs see: two services
// with ports, one volume.
const raceCompose = "services:\n  web: {}\n  db: {}\nvolumes:\n  data: {}\n"

func raceDocker() *docker.Docker {
	return docker.New(docker.WithRunner(composeConfigRunner{out: raceCompose}))
}

// raceGenerate is `git hop env generate` in the worktree of branch.
func raceGenerate(fs afero.Fs, hub, branch string) error {
	wt := filepath.Join(hub, "hops", branch)
	if err := fs.MkdirAll(wt, 0o755); err != nil {
		return err
	}
	env, err := GenerateWorktreeEnv(fs, raceDocker(), hub, hub, wt, branch, "acme", filepath.Base(hub))
	if err != nil {
		return err
	}
	if env == nil || len(env.Ports.Ports) != 2 {
		return fmt.Errorf("%s: no ports allocated: %+v", branch, env)
	}
	return nil
}

// raceRemove is what `git hop remove` does to ports.json and volumes.json.
func raceRemove(fs afero.Fs, hub, branch string) error {
	return DropEnvEntry(fs, hub, hub, filepath.Join(hub, "hops", branch), branch)
}

// raceMove is what `git hop move` does to them: branch becomes to.
func raceMove(fs afero.Fs, hub, branch, to string) error {
	return RekeyEnvEntry(fs, hub, hub, filepath.Join(hub, "hops", branch), filepath.Join(hub, "hops", to), branch, to)
}

// registerRaceHubs records each hub in state, as a separate repository,
// so every allocation sees the others' ports (LoadEnvRecords).
func registerRaceHubs(t *testing.T, fs afero.Fs, hubs ...string) {
	t.Helper()
	require.NoError(t, state.Update(fs, func(st *state.State) error {
		for i, hub := range hubs {
			id := "example.com/acme/" + filepath.Base(hub)
			st.AddRepository(id, &state.RepositoryState{
				URI: "https://" + id + ".git", Org: "acme", Repo: filepath.Base(hub),
				Worktrees: map[string]*state.WorktreeState{},
			})
			if err := st.AddHub(id, &state.HubState{Path: hub, Mode: state.HubModeLocal, CreatedAt: time.Unix(int64(i), 0)}); err != nil {
				return err
			}
		}
		return nil
	}))
}

func racePortsEntries(t *testing.T, fs afero.Fs, hub string) map[string]config.BranchPorts {
	t.Helper()
	cfg, err := config.NewLoader(fs).LoadPortsConfig(hub)
	require.NoError(t, err)
	return cfg.Branches
}

func raceVolumeKeys(t *testing.T, fs afero.Fs, hub string) []string {
	t.Helper()
	cfg, err := config.NewLoader(fs).LoadVolumesConfig(hub)
	require.NoError(t, err)
	return sortedKeysOf(cfg.Branches)
}

func sortedKeysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// raceBranches returns prefix-0 .. prefix-<n-1>, sorted.
func raceBranches(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s-%d", prefix, i)
	}
	sort.Strings(out)
	return out
}

// assertDistinctPorts fails for every port two entries, of any hub, hold.
func assertDistinctPorts(t *testing.T, fs afero.Fs, hubs ...string) {
	t.Helper()
	owner := map[int]string{}
	for _, hub := range hubs {
		for key, e := range racePortsEntries(t, fs, hub) {
			for svc, p := range e.Ports {
				who := filepath.Base(hub) + ":" + key + ":" + svc
				if prev, taken := owner[p]; taken {
					t.Errorf("port %d allocated twice: %s and %s", p, prev, who)
				}
				owner[p] = who
			}
		}
	}
}

// assertNoLeftovers fails for a temp file or lock file left in dir.
func assertNoLeftovers(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		if config.IsTempName(e.Name()) || strings.HasSuffix(e.Name(), ".lock") || strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("left behind in %s: %s", dir, e.Name())
		}
	}
}

// Worktrees set up at once, in one hub and in another, get distinct
// ports, and every allocation is recorded.
func TestGenerateWorktreeEnv_ConcurrentRunsGetDistinctPorts(t *testing.T) {
	const perRun = 12
	for _, tc := range []struct {
		name string
		fs   func(t *testing.T) (afero.Fs, string)
	}{
		{"memory", func(t *testing.T) (afero.Fs, string) { return afero.NewMemMapFs(), "/w" }},
		{"disk", func(t *testing.T) (afero.Fs, string) { return afero.NewOsFs(), t.TempDir() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs, root := tc.fs(t)
			hubA, hubB := filepath.Join(root, "a"), filepath.Join(root, "b")
			registerRaceHubs(t, fs, hubA, hubB)

			runs := []struct{ hub, prefix string }{{hubA, "x"}, {hubA, "y"}, {hubB, "z"}}
			var wg sync.WaitGroup
			errs := make(chan error, len(runs)*perRun)
			for _, r := range runs {
				wg.Add(1)
				go func(hub, prefix string) {
					defer wg.Done()
					for _, b := range raceBranches(prefix, perRun) {
						errs <- raceGenerate(fs, hub, b)
					}
				}(r.hub, r.prefix)
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}

			wantA := append(raceBranches("x", perRun), raceBranches("y", perRun)...)
			sort.Strings(wantA)
			assert.Equal(t, wantA, sortedKeysOf(racePortsEntries(t, fs, hubA)), "ports.json lost entries")
			assert.Equal(t, wantA, raceVolumeKeys(t, fs, hubA), "volumes.json lost entries")
			assert.Equal(t, raceBranches("z", perRun), sortedKeysOf(racePortsEntries(t, fs, hubB)))
			assertDistinctPorts(t, fs, hubA, hubB)
		})
	}
}

// startRaceHelper runs one TestEnvLockHelperProcess, as one git-hop
// process, with env. The helper's TestMain isolates it in homes of its
// own; it shares this test's state home, where the hubs are recorded, as
// git-hop runs of one user do.
func startRaceHelper(t *testing.T, env ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestEnvLockHelperProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(), append([]string{"ENVLOCK_HELPER_STATE=" + os.Getenv("XDG_STATE_HOME")}, env...)...)
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("helper %v:\n%s", env, out.String())
		}
	})
	return cmd
}

// The same as separate processes, on disk: three runs in two hubs.
func TestGenerateWorktreeEnv_ConcurrentProcessesGetDistinctPorts(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes")
	}
	const perRun = 15
	fs := afero.NewOsFs()
	root := t.TempDir()
	hubA, hubB := filepath.Join(root, "a"), filepath.Join(root, "b")
	registerRaceHubs(t, fs, hubA, hubB)

	cmds := []*exec.Cmd{
		startRaceHelper(t, "ENVLOCK_HELPER_HUB="+hubA, "ENVLOCK_HELPER_GEN=x", "ENVLOCK_HELPER_N="+strconv.Itoa(perRun)),
		startRaceHelper(t, "ENVLOCK_HELPER_HUB="+hubA, "ENVLOCK_HELPER_GEN=y", "ENVLOCK_HELPER_N="+strconv.Itoa(perRun)),
		startRaceHelper(t, "ENVLOCK_HELPER_HUB="+hubB, "ENVLOCK_HELPER_GEN=z", "ENVLOCK_HELPER_N="+strconv.Itoa(perRun)),
	}
	for _, cmd := range cmds {
		require.NoError(t, cmd.Wait())
	}

	wantA := append(raceBranches("x", perRun), raceBranches("y", perRun)...)
	sort.Strings(wantA)
	assert.Equal(t, wantA, sortedKeysOf(racePortsEntries(t, fs, hubA)), "ports.json lost entries")
	assert.Equal(t, wantA, raceVolumeKeys(t, fs, hubA), "volumes.json lost entries")
	assert.Equal(t, raceBranches("z", perRun), sortedKeysOf(racePortsEntries(t, fs, hubB)))
	assertDistinctPorts(t, fs, hubA, hubB)
	for _, dir := range []string{hubA, hubB, state.GetStateHome()} {
		assertNoLeftovers(t, dir)
	}
}

// Separate processes adding, removing and moving entries of one hub at
// once lose none of each other's changes.
func TestEnvEntries_ConcurrentAddRemoveMoveLoseNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes")
	}
	const adds, seeded = 15, 15
	fs := afero.NewOsFs()
	hub := filepath.Join(t.TempDir(), "hub")
	registerRaceHubs(t, fs, hub)
	drops, moves := raceBranches("drop", seeded), raceBranches("move", seeded)
	for _, b := range append(append([]string{}, drops...), moves...) {
		require.NoError(t, raceGenerate(fs, hub, b))
	}

	cmds := []*exec.Cmd{
		startRaceHelper(t, "ENVLOCK_HELPER_HUB="+hub, "ENVLOCK_HELPER_GEN=add", "ENVLOCK_HELPER_N="+strconv.Itoa(adds)),
		startRaceHelper(t, "ENVLOCK_HELPER_HUB="+hub, "ENVLOCK_HELPER_REMOVE="+strings.Join(drops, ",")),
		startRaceHelper(t, "ENVLOCK_HELPER_HUB="+hub, "ENVLOCK_HELPER_MOVE="+strings.Join(moves, ",")),
	}
	for _, cmd := range cmds {
		require.NoError(t, cmd.Wait())
	}

	want := append(raceBranches("add", adds), raceBranches("moved", seeded)...)
	sort.Strings(want)
	assert.Equal(t, want, sortedKeysOf(racePortsEntries(t, fs, hub)), "ports.json")
	assert.Equal(t, want, raceVolumeKeys(t, fs, hub), "volumes.json")
	assertDistinctPorts(t, fs, hub)
	assertNoLeftovers(t, hub)
	assertNoLeftovers(t, state.GetStateHome())
}

// TestEnvLockHelperProcess is one git-hop process of the tests above; it
// does nothing on its own.
func TestEnvLockHelperProcess(t *testing.T) {
	hub := os.Getenv("ENVLOCK_HELPER_HUB")
	if hub == "" {
		t.Skip("helper process")
	}
	t.Setenv("XDG_STATE_HOME", os.Getenv("ENVLOCK_HELPER_STATE"))
	fs := afero.NewOsFs()
	if list := os.Getenv("ENVLOCK_HELPER_REMOVE"); list != "" {
		for _, b := range strings.Split(list, ",") {
			require.NoError(t, raceRemove(fs, hub, b))
		}
		return
	}
	if list := os.Getenv("ENVLOCK_HELPER_MOVE"); list != "" {
		for _, b := range strings.Split(list, ",") {
			require.NoError(t, raceMove(fs, hub, b, "moved"+strings.TrimPrefix(b, "move")))
		}
		return
	}
	n, err := strconv.Atoi(os.Getenv("ENVLOCK_HELPER_N"))
	require.NoError(t, err)
	for _, b := range raceBranches(os.Getenv("ENVLOCK_HELPER_GEN"), n) {
		require.NoError(t, raceGenerate(fs, hub, b))
	}
}

// lockProbeRunner answers `docker compose config` like
// composeConfigRunner and records whether a lock file of locks was held
// by anyone while it ran.
type lockProbeRunner struct {
	out   string
	locks []string
	held  *[]string
}

func (r lockProbeRunner) Run(cmd string, args ...string) (string, error) {
	return r.RunInDir("", cmd, args...)
}

func (r lockProbeRunner) RunInDir(dir, cmd string, args ...string) (string, error) {
	for _, l := range r.locks {
		if filelock.Held(l) {
			*r.held = append(*r.held, filepath.Base(l))
		}
	}
	return r.out, nil
}

// docker compose can take its time; no run waits on it. Neither lock is
// held while GenerateWorktreeEnv asks docker compose for the config.
func TestGenerateWorktreeEnv_DockerRunsWithoutLocks(t *testing.T) {
	fs := afero.NewOsFs()
	hub := filepath.Join(t.TempDir(), "hub")
	wt := filepath.Join(hub, "hops", "main")
	require.NoError(t, fs.MkdirAll(wt, 0o755))
	var held []string
	d := docker.New(docker.WithRunner(lockProbeRunner{
		out:   raceCompose,
		locks: []string{filepath.Join(hub, PortsLockName), filepath.Join(state.GetStateHome(), AllocationLockName)},
		held:  &held,
	}))

	env, err := GenerateWorktreeEnv(fs, d, hub, hub, wt, "main", "acme", "hub")
	require.NoError(t, err)
	require.NotNil(t, env)
	assert.Empty(t, held, "docker compose ran under a lock")
	assert.Len(t, racePortsEntries(t, fs, hub), 1)
}
