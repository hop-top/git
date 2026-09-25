package cli

import (
	"os"

	"github.com/spf13/afero"
	"github.com/spf13/pflag"
	"hop.top/git/internal/config"
	"hop.top/git/internal/docker"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
	"hop.top/git/internal/services"
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
		HopspacePath: hop.ResolveHopspacePath(hubPath, hub.Config.Repo),
		Hub:          hub.Config,
	}, nil
}

// generateClonedEnv prepares the environment of the worktree a clone
// just checked out at the hub at hubPath, through the same path as add
// (services.GenerateWorktreeEnv): ports, volumes, .env and compose
// override. Clone runs it before post-worktree-add, as add does. Like
// add, it never fails the clone.
func generateClonedEnv(fs afero.Fs, hubPath string) {
	target, err := clonedEnvTarget(fs, hubPath)
	if err != nil {
		output.Warn("failed to prepare environment: %v", err)
		return
	}
	repo := target.Hub.Repo
	if _, err := services.GenerateWorktreeEnv(fs, docker.New(), target.HopspacePath, target.Root, target.Branch, repo.Org, repo.Repo); err != nil {
		output.Error("Failed to generate environment: %v", err)
	}
}

// startClonedEnv starts the environment generateClonedEnv prepared, once
// the clone is complete. A failed start only warns.
func startClonedEnv(fs afero.Fs, hubPath string, globalCfg *config.GlobalConfig) {
	target, err := clonedEnvTarget(fs, hubPath)
	if err != nil {
		services.WarnEnvNotStarted(err, hubPath)
		return
	}
	services.StartNewWorktreeEnv(fs, target, globalCfg, EventBus)
}
