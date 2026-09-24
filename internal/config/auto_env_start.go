package config

import "fmt"

// EnvAutoEnvStart overrides hop.env.autoStart for one process, the way
// GIT_HOP_ADD_FROM overrides hop.add.defaultStartPoint.
const EnvAutoEnvStart = "GIT_HOP_AUTO_ENV_START"

// ResolveAutoEnvStart decides whether add/clone start the environment of
// the worktree they create: --[no-]env-start (override) wins, then
// GIT_HOP_AUTO_ENV_START (env, ignored when empty), then hop.env.autoStart
// (configured, already defaulted). The env value takes git's boolean
// spellings; anything else is an error, as git dies on a bad boolean
// environment value rather than guessing.
func ResolveAutoEnvStart(override *bool, env string, configured bool) (bool, error) {
	if override != nil {
		return *override, nil
	}
	if env != "" {
		v, err := parseBool(env)
		if err != nil {
			return false, fmt.Errorf("bad boolean environment value '%s' for '%s'", env, EnvAutoEnvStart)
		}
		return v, nil
	}
	return configured, nil
}
