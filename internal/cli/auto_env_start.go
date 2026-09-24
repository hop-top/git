package cli

import (
	"os"

	"github.com/spf13/afero"
	"github.com/spf13/pflag"
	"hop.top/git/internal/config"
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
		"start the new worktree's environment (default: hop.autoEnvStart)")
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
		configured = globalCfg.Defaults.AutoEnvStart
	}
	on, err := config.ResolveAutoEnvStart(override, os.Getenv(config.EnvAutoEnvStart), configured)
	if err != nil {
		output.FatalCode(exitFatal, "%v", err)
	}
	return on
}

// startClonedEnv starts the environment of the worktree a clone just
// checked out at the hub at hubPath. Like add, it never fails the clone.
func startClonedEnv(fs afero.Fs, hubPath string, globalCfg *config.GlobalConfig) {
	hub, err := hop.LoadHub(fs, hubPath)
	if err != nil {
		services.WarnEnvNotStarted(err, hubPath)
		return
	}
	branch := hub.Config.Repo.DefaultBranch
	services.StartNewWorktreeEnv(fs, services.EnvTarget{
		Root:         resolveSwitchWorktreePath(hub.Config.Branches[branch], hubPath),
		Branch:       branch,
		HopspacePath: hop.ResolveHopspacePath(hubPath, hub.Config.Repo),
		Hub:          hub.Config,
	}, globalCfg, EventBus)
}
