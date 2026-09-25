package services

import (
	"context"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/docker"
	"hop.top/git/internal/events"
	"hop.top/git/internal/output"
	"hop.top/kit/go/runtime/bus"
)

// WorktreeSetup is what SetUpWorktree prepared for a new worktree.
type WorktreeSetup struct {
	Target EnvTarget
	// Env is the generated Docker environment; nil when the worktree has
	// none or generation failed.
	Env *WorktreeEnv
	// DepsInstalled reports that a package manager was detected and its
	// shared dependencies were installed and linked.
	DepsInstalled bool
}

// SetUpWorktree prepares a worktree add or clone has just created, before
// its post-worktree-add hook fires, so every hook finds it ready: the
// Docker environment first (GenerateWorktreeEnv: ports, volumes, .env and
// compose override), then the shared dependencies. Both steps report their
// own failures and neither fails the command.
func SetUpWorktree(fs afero.Fs, d *docker.Docker, t EnvTarget, globalConfig *config.GlobalConfig) WorktreeSetup {
	s := WorktreeSetup{Target: t}
	var org, repo string
	if t.Hub != nil {
		org, repo = t.Hub.Repo.Org, t.Hub.Repo.Repo
	}
	env, err := GenerateWorktreeEnv(fs, d, t.HopspacePath, t.Root, t.Branch, org, repo)
	if err != nil {
		output.Error("Failed to generate environment: %v", err)
	} else {
		s.Env = env
	}
	s.DepsInstalled = setUpWorktreeDeps(fs, t, globalConfig)
	return s
}

// setUpWorktreeDeps installs and links the shared dependencies of t and
// reports whether it did. The "Setting up dependencies..." /
// "Dependencies installed." pair is gated on a package manager actually
// being detected: for a plain project it would be misleading noise.
func setUpWorktreeDeps(fs afero.Fs, t EnvTarget, globalConfig *config.GlobalConfig) bool {
	dm, err := NewDepsManager(fs, t.HopspacePath, globalConfig)
	if err != nil {
		output.Warn("Failed to initialize dependency manager: %v", err)
		return false
	}
	detected, err := dm.DetectInWorktree(t.Root)
	if err != nil {
		output.Warn("Failed to detect package managers: %v", err)
		return false
	}
	if len(detected) == 0 {
		return false
	}
	output.Info("Setting up dependencies...")
	if err := dm.EnsureDeps(t.Root, t.Branch); err != nil {
		output.Warn("Failed to ensure dependencies: %v", err)
		return false
	}
	output.Info("Dependencies installed.")
	return true
}

// PublishCreated emits worktree.created for the set-up worktree of the
// hub at repoPath, then deps.installed when SetUpWorktree installed
// dependencies: the order every command that creates a worktree publishes
// them in. It is separate from SetUpWorktree so each command publishes
// once the worktree is registered.
func (s WorktreeSetup) PublishCreated(ctx context.Context, b bus.Bus, repoPath string) {
	if b == nil {
		return
	}
	_ = b.Publish(ctx, bus.NewEvent(
		events.WorktreeCreated, events.Source,
		events.WorktreeEvent{
			Path:         s.Target.Root,
			Branch:       s.Target.Branch,
			HopspacePath: s.Target.HopspacePath,
			RepoPath:     repoPath,
		},
	))
	s.PublishDepsInstalled(ctx, b)
}

// PublishDepsInstalled emits deps.installed on b when SetUpWorktree
// installed dependencies. PublishCreated calls it after worktree.created.
func (s WorktreeSetup) PublishDepsInstalled(ctx context.Context, b bus.Bus) {
	if !s.DepsInstalled || b == nil {
		return
	}
	_ = b.Publish(ctx, bus.NewEvent(
		events.DepsInstalled, events.Source,
		events.DepsEvent{WorktreePath: s.Target.Root, Branch: s.Target.Branch},
	))
}
