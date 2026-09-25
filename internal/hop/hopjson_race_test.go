package hop_test

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

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/hop"
)

// hop.json is rewritten by every add, remove, merge, move, repair and
// doctor --fix. Two git-hop processes working in one hub must not undo
// each other: a branch one removes stays removed, a branch one adds stays
// added, whatever the other had loaded before.

func newRaceHub(t *testing.T, fs afero.Fs, hubPath string, seeded ...string) {
	t.Helper()
	hub, err := hop.CreateHub(fs, hubPath, "https://github.com/org/repo.git", "org", "repo", "main")
	require.NoError(t, err)
	for _, b := range seeded {
		require.NoError(t, hub.AddBranch(b, b, filepath.Join(hubPath, "hops", b)))
	}
}

func hubBranches(t *testing.T, fs afero.Fs, hubPath string) []string {
	t.Helper()
	hub, err := hop.LoadHub(fs, hubPath)
	require.NoError(t, err)
	var names []string
	for name := range hub.Config.Branches {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// The observed failure: add loads hop.json when it starts and writes it
// after creating the worktree, running hooks and installing deps. A
// remove that finished in between is undone by that write.
func TestHub_WriteFromStaleLoadKeepsOtherWritersChanges(t *testing.T) {
	fs := afero.NewMemMapFs()
	newRaceHub(t, fs, "/hub", "gone")

	adder, err := hop.LoadHub(fs, "/hub")
	require.NoError(t, err)

	remover, err := hop.LoadHub(fs, "/hub")
	require.NoError(t, err)
	require.NoError(t, remover.RemoveBranch("gone"))

	require.NoError(t, adder.AddBranch("new", "new", "/hub/hops/new"))

	assert.Equal(t, []string{"new"}, hubBranches(t, fs, "/hub"))
}

// A hub is its own hopspace by default, so the hopspace side of add
// writes the same hop.json and must not resurrect a removed branch
// either.
func TestHopspace_WriteFromStaleLoadKeepsOtherWritersChanges(t *testing.T) {
	fs := afero.NewMemMapFs()
	newRaceHub(t, fs, "/hub", "gone")

	adderSpace, err := hop.LoadHopspace(fs, "/hub")
	require.NoError(t, err)

	remover, err := hop.LoadHub(fs, "/hub")
	require.NoError(t, err)
	require.NoError(t, remover.RemoveBranch("gone"))

	require.NoError(t, adderSpace.RegisterBranch(adderSpace.Path, "new", "/hub/hops/new"))

	assert.Equal(t, []string{"new"}, hubBranches(t, fs, "/hub"))
}

// runAddsAndRemoves adds adders*perAdder branches and removes every
// seeded branch, from concurrent goroutines, each operation on a hub it
// loads itself as a separate git-hop run would.
func runAddsAndRemoves(t *testing.T, fs afero.Fs, hubPath string, adders, perAdder int, seeded []string) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make(chan error, adders*perAdder*2+len(seeded))
	for a := 0; a < adders; a++ {
		wg.Add(1)
		go func(a int) {
			defer wg.Done()
			for i := 0; i < perAdder; i++ {
				errs <- addLikeGitHopAdd(fs, hubPath, fmt.Sprintf("add-%d-%d", a, i))
			}
		}(a)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, b := range seeded {
			errs <- removeLikeGitHopRemove(fs, hubPath, b)
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

func addLikeGitHopAdd(fs afero.Fs, hubPath, branch string) error {
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		return err
	}
	space, err := hop.LoadHopspace(fs, hubPath)
	if err != nil {
		return err
	}
	path := filepath.Join(hubPath, "hops", branch)
	if err := space.RegisterBranch(space.Path, branch, path); err != nil {
		return err
	}
	return hub.AddBranch(branch, branch, path)
}

func removeLikeGitHopRemove(fs afero.Fs, hubPath, branch string) error {
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		return err
	}
	if err := hub.RemoveBranch(branch); err != nil {
		return err
	}
	space, err := hop.LoadHopspace(fs, hubPath)
	if err != nil {
		return err
	}
	return space.UnregisterBranch(space.Path, branch, "")
}

func expectedAfter(adders, perAdder int) []string {
	var want []string
	for a := 0; a < adders; a++ {
		for i := 0; i < perAdder; i++ {
			want = append(want, fmt.Sprintf("add-%d-%d", a, i))
		}
	}
	sort.Strings(want)
	return want
}

func seededBranches(n int) []string {
	var seeded []string
	for i := 0; i < n; i++ {
		seeded = append(seeded, fmt.Sprintf("seed-%d", i))
	}
	return seeded
}

func TestHub_ConcurrentAddRemoveLosesNothing(t *testing.T) {
	const adders, perAdder = 4, 15
	seeded := seededBranches(23)
	for _, tc := range []struct {
		name string
		fs   func(t *testing.T) (afero.Fs, string)
	}{
		{"memory", func(t *testing.T) (afero.Fs, string) { return afero.NewMemMapFs(), "/hub" }},
		{"disk", func(t *testing.T) (afero.Fs, string) { return afero.NewOsFs(), t.TempDir() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs, hubPath := tc.fs(t)
			newRaceHub(t, fs, hubPath, seeded...)
			runAddsAndRemoves(t, fs, hubPath, adders, perAdder, seeded)
			assert.Equal(t, expectedAfter(adders, perAdder), hubBranches(t, fs, hubPath))
		})
	}
}

// The incident, as separate processes: sequential removes in one, adds
// in two others, all in one hub on disk.
func TestHub_ConcurrentProcessesLoseNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes")
	}
	const adders, perAdder = 2, 25
	seeded := seededBranches(23)
	hubPath := t.TempDir()
	fs := afero.NewOsFs()
	newRaceHub(t, fs, hubPath, seeded...)

	var cmds []*exec.Cmd
	start := func(env ...string) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHopJSONHelperProcess$", "-test.count=1")
		cmd.Env = append(os.Environ(), append([]string{"HOPJSON_HELPER_HUB=" + hubPath}, env...)...)
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
		start("HOPJSON_HELPER_ADD="+strconv.Itoa(a), "HOPJSON_HELPER_N="+strconv.Itoa(perAdder))
	}
	start("HOPJSON_HELPER_REMOVE=" + strings.Join(seeded, ","))
	for _, cmd := range cmds {
		require.NoError(t, cmd.Wait())
	}

	assert.Equal(t, expectedAfter(adders, perAdder), hubBranches(t, fs, hubPath))
}

// TestHopJSONHelperProcess is one git-hop process of
// TestHub_ConcurrentProcessesLoseNothing; it does nothing on its own.
func TestHopJSONHelperProcess(t *testing.T) {
	hubPath := os.Getenv("HOPJSON_HELPER_HUB")
	if hubPath == "" {
		t.Skip("helper process")
	}
	fs := afero.NewOsFs()
	if list := os.Getenv("HOPJSON_HELPER_REMOVE"); list != "" {
		for _, b := range strings.Split(list, ",") {
			require.NoError(t, removeLikeGitHopRemove(fs, hubPath, b))
		}
		return
	}
	n, err := strconv.Atoi(os.Getenv("HOPJSON_HELPER_N"))
	require.NoError(t, err)
	for i := 0; i < n; i++ {
		branch := fmt.Sprintf("add-%s-%d", os.Getenv("HOPJSON_HELPER_ADD"), i)
		require.NoError(t, addLikeGitHopAdd(fs, hubPath, branch))
	}
}
