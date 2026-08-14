package e2e

import (
	"bufio"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/services"
)

// e2eNetworkPrefix is the compose-project prefix every e2e-created docker
// network inherits. Test root dirs are created as os.MkdirTemp(..,
// "git-hop-e2e-*"), that basename becomes the compose project's org segment,
// and compose names the network "<project>_<network>". So every network this
// suite creates -- and nothing else on the machine -- starts with this.
//
// SweepStaleNetworks matches on this prefix with a mandatory trailing
// separator (see stale network matching there), so renaming this constant
// can only ever narrow the match, never widen it into unrelated networks.
const e2eNetworkPrefix = "git-hop-e2e-"

// dockerAvailable reports whether a docker daemon is reachable. Every cleanup
// path routes through this so a machine without docker runs the non-docker
// e2e tests untouched rather than erroring on missing binaries.
func dockerAvailable() bool {
	return exec.Command("docker", "info").Run() == nil
}

// StopDockerEnv tears down the compose stack for the worktree at dir, which
// holds the hop branch named by branch.
//
// It resolves the SAME compose project name that `git hop env start` injects
// via -p (org-repo-branch, derived from the enclosing hub config) and passes
// it to `compose down`. Without -p, compose falls back to the cwd basename --
// a different project entirely -- so the down targets a stack that does not
// exist and silently leaves the real network behind. That mismatch is what
// leaked a network per run.
//
// `down` rather than `stop`: `stop` halts containers but leaves the network
// allocated, which still consumes an address pool slot. `down` removes the
// containers and the project's own network in the correct order.
//
// Safe to call when nothing is running; errors are logged, not fatal.
func StopDockerEnv(t *testing.T, dir, branch string) {
	t.Helper()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return
	}
	if !dockerAvailable() {
		return
	}

	args := []string{"compose"}
	if project := hopComposeProject(dir, branch); project != "" {
		args = append(args, "-p", project)
	}
	args = append(args, "down", "--volumes", "--remove-orphans")

	cmd := exec.Command("docker", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("docker %v in %s: %v\n%s", args, dir, err, out)
	}
}

// hopComposeProject derives the compose project name hop uses for the
// worktree at dir holding branch. Returns "" when the enclosing hub config
// cannot be loaded, which makes the caller fall back to compose's default --
// the same behavior hop itself has when the config is unavailable.
func hopComposeProject(dir, branch string) string {
	fs := afero.NewOsFs()
	hubPath, err := hop.FindHub(fs, dir)
	if err != nil {
		return ""
	}
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		return ""
	}
	return services.ComposeProjectName(hub.Config.Repo.Org, hub.Config.Repo.Repo, branch)
}

// SweepStaleNetworks removes docker networks left over from a previous run of
// this suite.
//
// Per-test cleanup cannot cover a killed process: Ctrl-C, a CI timeout or a
// panic skips t.Cleanup entirely, and those orphans accumulate until docker
// exhausts its predefined address pools. That surfaces as an unrelated-looking
// test failure ("all predefined address pools have been fully subnetted"), so
// a run must not inherit the previous run's debris.
//
// MUST be called from TestMain before any test starts. Tests run in parallel,
// so sweeping from per-test cleanup would delete networks belonging to tests
// still running.
func SweepStaleNetworks() {
	if !dockerAvailable() {
		return
	}

	out, err := exec.Command("docker", "network", "ls", "--format", "{{.Name}}").Output()
	if err != nil {
		return
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if !isE2ENetwork(name) {
			continue
		}
		// Best-effort: a network with containers still attached refuses
		// removal. Those belong to a stack that is somehow still up, and
		// leaving them is strictly safer than forcing.
		_ = exec.Command("docker", "network", "rm", name).Run()
	}
}

// isE2ENetwork reports whether name is a docker network this suite created.
//
// The match requires the prefix AND at least one more character, so it can
// never degenerate into a bare-prefix match that swallows unrelated networks.
// Nothing outside this suite names a network "git-hop-e2e-<something>": the
// prefix comes from the suite's own os.MkdirTemp pattern.
func isE2ENetwork(name string) bool {
	return strings.HasPrefix(name, e2eNetworkPrefix) && len(name) > len(e2eNetworkPrefix)
}
