package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/config"
	"hop.top/git/internal/docker"
	"hop.top/git/internal/hop"
)

// composeConfigRunner answers `docker compose config` with a fixed config,
// so Generate needs no docker daemon and no compose file on the real disk.
type composeConfigRunner struct{ out string }

func (r composeConfigRunner) Run(cmd string, args ...string) (string, error) {
	return r.RunInDir("", cmd, args...)
}

func (r composeConfigRunner) RunInDir(dir, cmd string, args ...string) (string, error) {
	return r.out, nil
}

const hardcodedPortsCompose = "services:\n  web:\n    image: nginx\n    ports:\n      - \"8080:80\"\n"

func newMemEnvManager(t *testing.T, fs afero.Fs) *EnvManager {
	t.Helper()
	d := docker.New(docker.WithRunner(composeConfigRunner{out: "services:\n  web: {}\n"}))
	return NewEnvManager(fs,
		&config.PortsConfig{
			AllocationMode: "incremental",
			BaseRange:      config.PortRange{Start: 10000, End: 20000},
			Branches:       map[string]config.BranchPorts{},
		},
		&config.VolumesConfig{BasePath: "/hopspace/volumes", Branches: map[string]config.BranchVolumes{}},
		d)
}

func onRealDisk(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// TestEnvManagerGenerate_UsesInjectedFs: a compose file that exists only on
// the injected fs is found and read, and the override plus its cache meta
// land on that fs, not on the real disk.
func TestEnvManagerGenerate_UsesInjectedFs(t *testing.T) {
	fs := afero.NewMemMapFs()
	wt := "/hops/feat-x"
	require.NoError(t, afero.WriteFile(fs, filepath.Join(wt, "compose.yaml"), []byte(hardcodedPortsCompose), 0644))

	_, _, overridePath, err := newMemEnvManager(t, fs).Generate("feat-x", wt, "acme", "svc")
	require.NoError(t, err)

	want := hop.GetComposeOverrideCachePath("acme", "svc", "feat-x")
	require.Equal(t, want, overridePath, "compose file on the injected fs was not found or read")

	override, err := afero.ReadFile(fs, overridePath)
	require.NoError(t, err, "override not written to the injected fs")
	assert.Contains(t, string(override), "HOP_PORT_WEB")

	metaPath := hop.GetOverrideMetaCachePath("acme", "svc", "feat-x")
	ok, err := afero.Exists(fs, metaPath)
	require.NoError(t, err)
	assert.True(t, ok, "override meta not written to the injected fs")

	assert.False(t, onRealDisk(filepath.Dir(overridePath)), "override cache dir created on the real disk: %s", filepath.Dir(overridePath))
	assert.False(t, onRealDisk(overridePath), "override leaked to the real disk: %s", overridePath)
	assert.False(t, onRealDisk(metaPath), "override meta leaked to the real disk: %s", metaPath)

	env, err := afero.ReadFile(fs, filepath.Join(wt, ".env"))
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(env), "HOP_PORT_WEB="), ".env missing HOP_PORT_WEB:\n%s", env)
}

// TestEnvManagerGenerate_ReadsOverrideMetaFromInjectedFs: an up-to-date
// meta on the injected fs keeps the cached override; Generate must not
// rewrite it.
func TestEnvManagerGenerate_ReadsOverrideMetaFromInjectedFs(t *testing.T) {
	fs := afero.NewMemMapFs()
	wt := "/hops/feat-y"
	require.NoError(t, afero.WriteFile(fs, filepath.Join(wt, "compose.yaml"), []byte(hardcodedPortsCompose), 0644))

	sum := sha256.Sum256([]byte(hardcodedPortsCompose))
	meta, err := json.Marshal(overrideMeta{ComposeHash: hex.EncodeToString(sum[:])})
	require.NoError(t, err)
	overridePath := hop.GetComposeOverrideCachePath("acme", "svc", "feat-y")
	require.NoError(t, afero.WriteFile(fs, hop.GetOverrideMetaCachePath("acme", "svc", "feat-y"), meta, 0644))
	require.NoError(t, afero.WriteFile(fs, overridePath, []byte("# cached\n"), 0644))

	_, _, got, err := newMemEnvManager(t, fs).Generate("feat-y", wt, "acme", "svc")
	require.NoError(t, err)
	require.Equal(t, overridePath, got)

	override, err := afero.ReadFile(fs, overridePath)
	require.NoError(t, err)
	assert.Equal(t, "# cached\n", string(override), "up-to-date override was regenerated; meta not read from the injected fs")
	assert.False(t, onRealDisk(overridePath), "override leaked to the real disk: %s", overridePath)
}
