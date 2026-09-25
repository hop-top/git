package cli

import (
	"context"
	"os"

	"github.com/spf13/afero"
	"github.com/spf13/pflag"
	"hop.top/git/internal/config"
	"hop.top/git/internal/docker"
	"hop.top/git/internal/events"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
	"hop.top/kit/go/runtime/bus"
)

// exitFatal is git's status for a fatal error.
const exitFatal = 128

// cloneEnvStartFlag / cloneNoEnvStartFlag hold clone's --[no-]env-start.
var (
	cloneEnvStartFlag   bool
	cloneNoEnvStartFlag bool
)

// AddEnvStartFlags registers the --env-start / --no-env-start pair of a
// command that creates a worktree; read it back with NegatableFlag.
func AddEnvStartFlags(flags *pflag.FlagSet, yes, no *bool) {
	flags.BoolVar(yes, "env-start", false,
		"start the new worktree's environment (default: hop.env.autoStart)")
	flags.BoolVar(no, "no-env-start", false,
		"do not start the new worktree's environment")
}

// DecideAutoEnvStart reports whether the command starts the environment
// of the worktree it creates. See config.ResolveAutoEnvStart for the
// precedence; a bad GIT_HOP_AUTO_ENV_START is fatal, so callers resolve
// this before their first write.
func DecideAutoEnvStart(override *bool, globalCfg *config.GlobalConfig) bool {
	configured := false
	if globalCfg != nil {
		configured = globalCfg.Defaults.EnvAutoStart
	}
	on, err := config.ResolveAutoEnvStart(override, os.Getenv(config.EnvAutoEnvStart), configured)
	if err != nil {
		output.FatalCode(exitFatal, "%v", err)
	}
	return on
}

// clonedEnvTarget resolves the default-branch worktree of the hub a
// clone just wrote at hubPath.
func clonedEnvTarget(fs afero.Fs, hubPath string) (services.EnvTarget, error) {
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		return services.EnvTarget{}, err
	}
	branch := hub.Config.Repo.DefaultBranch
	return services.EnvTarget{
		Root:         resolveSwitchWorktreePath(hub.Config.Branches[branch], hubPath),
		Branch:       branch,
		HubPath:      hubPath,
		HopspacePath: hop.ResolveHopspacePath(hubPath, hub.Config.Repo),
		Hub:          hub.Config,
	}, nil
}

// setUpClonedWorktree prepares the worktree a clone just checked out at
// the hub at hubPath through the same path as add (services.SetUpWorktree):
// ports, volumes, .env and compose override, then shared deps. Clone runs
// it before post-worktree-add, as add does. Like add, it never fails the
// clone.
//
// The hub and worktree are registered by then, so it also publishes their
// events: hopspace.initialized for the hub, as init does for the hub it
// registers, then worktree.created and deps.installed, as add does.
func setUpClonedWorktree(fs afero.Fs, hubPath string, globalCfg *config.GlobalConfig) {
	target, err := clonedEnvTarget(fs, hubPath)
	if err != nil {
		output.Warn("failed to prepare environment: %v", err)
		return
	}
	setup := services.SetUpWorktree(fs, docker.New(), target, globalCfg)

	ctx := context.Background()
	_ = EventBus.Publish(ctx, bus.NewEvent(
		events.HopspaceInitialized, events.Source,
		events.HopspaceEvent{
			Path: hubPath,
			Org:  target.Hub.Repo.Org,
			Repo: target.Hub.Repo.Repo,
		},
	))
	setup.PublishCreated(ctx, EventBus, hubPath)
}

// startClonedEnv starts the environment setUpClonedWorktree prepared, once
// the clone is complete. A failed start only warns. The start is progress
// beside the clone's result, so it goes to stderr.
func startClonedEnv(fs afero.Fs, hubPath string, globalCfg *config.GlobalConfig) {
	target, err := clonedEnvTarget(fs, hubPath)
	if err != nil {
		services.WarnEnvNotStarted(err, hubPath)
		return
	}
	target.Progress = os.Stderr
	services.StartNewWorktreeEnv(fs, target, globalCfg, EventBus)
}
