package cmd

import (
	"path/filepath"

	"github.com/spf13/afero"

	"hop.top/git/internal/config"
	"hop.top/git/internal/docker"
	"hop.top/git/internal/services"
)

// envStartResult is the result of `git hop env start`: the worktree's
// environment once the start has run. envStopResult is the same for
// `git hop env stop`; the two differ only in the name of the flag that
// says whether the command did anything, so one converts to the other.
type envStartResult struct {
	Branch   string          `json:"branch" yaml:"branch" table:"branch" jsonschema:"description=Branch checked out in the worktree"`
	Path     string          `json:"path" yaml:"path" table:"path" jsonschema:"description=Absolute path of the worktree"`
	Manager  string          `json:"manager" yaml:"manager" table:"manager" jsonschema:"description=Environment manager that ran (for example docker-compose); empty when the worktree has no environment"`
	Done     bool            `json:"env_started" yaml:"env_started" table:"env_started" jsonschema:"description=True when the environment was started; false when the worktree has none"`
	Ports    map[string]int  `json:"ports,omitempty" yaml:"ports,omitempty" jsonschema:"description=Port allocated to each service; absent when none are allocated"`
	Services []statusService `json:"services,omitempty" yaml:"services,omitempty" jsonschema:"description=Docker Compose containers of the worktree after the start, as docker compose ps reports them; absent for other managers or when compose reports none"`
}

// envStopResult is the result of `git hop env stop`; see envStartResult.
type envStopResult struct {
	Branch   string          `json:"branch" yaml:"branch" table:"branch" jsonschema:"description=Branch checked out in the worktree"`
	Path     string          `json:"path" yaml:"path" table:"path" jsonschema:"description=Absolute path of the worktree"`
	Manager  string          `json:"manager" yaml:"manager" table:"manager" jsonschema:"description=Environment manager that ran (for example docker-compose); empty when the worktree has no environment"`
	Done     bool            `json:"env_stopped" yaml:"env_stopped" table:"env_stopped" jsonschema:"description=True when the environment was stopped; false when the worktree has none"`
	Ports    map[string]int  `json:"ports,omitempty" yaml:"ports,omitempty" jsonschema:"description=Port allocated to each service; absent when none are allocated"`
	Services []statusService `json:"services,omitempty" yaml:"services,omitempty" jsonschema:"description=Docker Compose containers of the worktree after the stop, as docker compose ps reports them; absent for other managers or when compose reports none"`
}

// envGenerateResult is the result of `git hop env generate`.
type envGenerateResult struct {
	Branch    string         `json:"branch" yaml:"branch" table:"branch" jsonschema:"description=Branch checked out in the worktree"`
	Path      string         `json:"path" yaml:"path" table:"path" jsonschema:"description=Absolute path of the worktree"`
	Generated bool           `json:"generated" yaml:"generated" table:"generated" jsonschema:"description=True when the environment files were written; false when the worktree has no Docker environment"`
	EnvFile   string         `json:"env_file" yaml:"env_file" table:"env_file" jsonschema:"description=Absolute path of the .env written in the worktree; empty when not generated"`
	Override  string         `json:"override" yaml:"override" table:"override" jsonschema:"description=Absolute path of the compose override for the branch (it replaces the compose file's hardcoded host ports); empty when the compose file needs none"`
	Ports     map[string]int `json:"ports,omitempty" yaml:"ports,omitempty" jsonschema:"description=Port allocated to each service; absent when none are allocated"`
}

// newEnvResult describes t's environment after a start or stop run by
// manager ("" when t has none): the branch, the ports allocated to it and,
// for docker-compose, its containers as compose reports them.
func newEnvResult(fs afero.Fs, d *docker.Docker, t services.EnvTarget, branch, manager string, done bool) envStartResult {
	res := envStartResult{Branch: branch, Path: t.Root, Manager: manager, Done: done}
	if t.HopspacePath != "" && branch != "" {
		if portsCfg, _ := config.NewLoader(fs).LoadPortsConfig(t.HopspacePath); portsCfg != nil {
			if bp, ok := portsCfg.Branches[branch]; ok && len(bp.Ports) > 0 {
				res.Ports = bp.Ports
			}
		}
	}
	if manager == "docker-compose" {
		var org, repo string
		if t.Hub != nil {
			org, repo = t.Hub.Repo.Org, t.Hub.Repo.Repo
		}
		if ps, err := d.ComposePs(t.Root, services.ComposeProjectName(org, repo, branch)); err == nil {
			if svcs := parseComposePs(ps); len(svcs) > 0 {
				res.Services = svcs
			}
		}
	}
	return res
}

// newEnvGenerateResult is the result of generating env for the worktree
// at root; env is nil when it has no Docker environment.
func newEnvGenerateResult(root, branch string, env *services.WorktreeEnv) envGenerateResult {
	res := envGenerateResult{Branch: branch, Path: root}
	if env == nil {
		return res
	}
	res.Generated = true
	// services.GenerateWorktreeEnv writes the .env at the worktree root.
	res.EnvFile = filepath.Join(root, ".env")
	res.Override = env.OverridePath
	if env.Ports != nil && len(env.Ports.Ports) > 0 {
		res.Ports = env.Ports.Ports
	}
	return res
}
