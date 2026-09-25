package services

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/events"
	"hop.top/git/internal/output"
	"hop.top/kit/go/runtime/bus"
)

// ErrNoEnvironment reports a worktree with no environment to start: no
// manager's detect file is present and the hub names none.
var ErrNoEnvironment = errors.New("no environment manager detected")

// EnvTarget is the worktree whose environment is started or stopped.
// HopspacePath and Hub are empty for a worktree outside a hub.
type EnvTarget struct {
	Root   string
	Branch string
	// HubPath is the hub the worktree belongs to; empty outside a hub.
	HubPath      string
	HopspacePath string
	Hub          *config.HubConfig
	// Progress, when set, receives everything a start or stop says: the
	// status lines, the manager's steps, hook output and the lifecycle
	// command's stdout. `git hop env start/stop` and the automatic start
	// after add and clone set it to stderr: a start has no result of its
	// own, and add's and clone's results are theirs alone. Nil keeps the caller's streams: status via
	// output.Info, the rest on stdout, or stderr under a structured result.
	Progress io.Writer
}

// status prints one status line of a start: on t.Progress, for a person
// at a terminal, when set; through output.Info otherwise.
func (t EnvTarget) status(format string, args ...any) {
	if t.Progress == nil {
		output.Info(format, args...)
		return
	}
	output.NoteTo(t.Progress, format, args...)
}

// ResolveEnv picks the environment manager for t and the compose override
// cached for its branch ("" when none was generated). A nil manager means
// t has no environment.
//
// The override is the one the branch's ports.json entry names; an entry
// an earlier release wrote names none and uses the repository-wide one.
func ResolveEnv(t EnvTarget, globalConfig *config.GlobalConfig) (*EnvironmentManager, string, error) {
	managers, err := LoadEnvManagers(globalConfig)
	if err != nil {
		return nil, "", err
	}
	manager, err := DetectEnvManager(t.Root, t.Hub, managers)
	if err != nil || manager == nil {
		return nil, "", err
	}
	// Results carry stdout alone when a structured result is requested;
	// otherwise the manager's chatter goes where the hooks' does.
	switch {
	case t.Progress != nil:
		manager.Out = t.Progress
	case output.IsStructured():
		manager.Out = os.Stderr
	}

	return manager, resolveOverridePath(t), nil
}

// resolveOverridePath returns the compose override t's branch uses, or ""
// when there is none on disk.
func resolveOverridePath(t EnvTarget) string {
	if t.Hub == nil || t.Hub.Repo.Org == "" || t.Hub.Repo.Repo == "" || t.Branch == "" {
		return ""
	}
	dir := legacyOverrideDir(t.Hub.Repo.Org, t.Hub.Repo.Repo, t.Branch)
	if t.HopspacePath != "" {
		if cfg, err := config.NewLoader(afero.NewOsFs()).LoadPortsConfig(t.HopspacePath); err == nil {
			if entry, ok := cfg.Branches[t.Branch]; ok && entry.OverrideDir != "" {
				dir = entry.OverrideDir
			}
		}
	}
	candidate := filepath.Join(dir, overrideFileName)
	if _, err := os.Stat(candidate); err != nil {
		return ""
	}
	return candidate
}

// StartEnv starts t's environment. It is the one start path: `git hop env
// start` and the automatic start after add and clone both run it. Shared
// dependencies are ensured first (a failure there only warns), then the
// manager's start command runs between its hooks, and env.started is
// published on b (nil skips it). A worktree without an environment
// returns ErrNoEnvironment and starts nothing.
func StartEnv(fs afero.Fs, t EnvTarget, globalConfig *config.GlobalConfig, b bus.Bus) error {
	manager, overridePath, err := ResolveEnv(t, globalConfig)
	if err != nil {
		return err
	}
	if manager == nil {
		return ErrNoEnvironment
	}

	t.status("Environment Manager: %s", manager.Name)

	if t.HopspacePath != "" && t.Branch != "" {
		t.status("Ensuring dependencies...")
		depsManager, err := NewDepsManager(fs, t.HopspacePath, globalConfig)
		if err != nil {
			output.Warn("Failed to initialize dependency manager: %v", err)
		} else if err := depsManager.EnsureDeps(t.Root, t.Branch); err != nil {
			output.Warn("Failed to ensure dependencies: %v", err)
		} else {
			t.status("Dependencies ready.")
		}
	}

	if err := manager.Start(t.Root, t.Branch, t.HopspacePath, t.Hub, overridePath); err != nil {
		return err
	}
	if b != nil {
		_ = b.Publish(context.Background(), bus.NewEvent(
			events.EnvStarted, events.Source,
			events.EnvEvent{Action: "start", Root: t.Root, Branch: t.Branch},
		))
	}
	return nil
}

// StartNewWorktreeEnv runs StartEnv for a worktree add or clone has just
// created and reports whether the environment started. The worktree is
// already complete at this point, so nothing here fails the command: a
// worktree without an environment is skipped silently, and a failed start
// warns and points at `git hop env start`.
func StartNewWorktreeEnv(fs afero.Fs, t EnvTarget, globalConfig *config.GlobalConfig, b bus.Bus) bool {
	err := StartEnv(fs, t, globalConfig, b)
	switch {
	case err == nil:
		return true
	case errors.Is(err, ErrNoEnvironment):
		return false
	default:
		WarnEnvNotStarted(err, t.Root)
		return false
	}
}

// WarnEnvNotStarted reports an automatic start of root's environment that
// failed after the worktree itself was created.
// It is never silent: -q, which held back the start's own output until
// the failure replayed it, keeps the warning too.
func WarnEnvNotStarted(err error, root string) {
	output.WarnAlways("failed to start environment: %v", err)
	output.Hint("the worktree is ready; start its environment with 'git hop env start' from %s", root)
}
