// Package events defines topic constants and payload structs for the
// git-hop event bus.
package events

import "hop.top/kit/bus"

// Topic constants follow dot-separated MQTT convention.
const (
	WorktreeCreated bus.Topic = "worktree.created"
	WorktreeRemoved bus.Topic = "worktree.removed"
	WorktreeMerged  bus.Topic = "worktree.merged"
	WorktreeMoved   bus.Topic = "worktree.moved"

	EnvStarted bus.Topic = "env.started"
	EnvStopped bus.Topic = "env.stopped"

	HopspaceInitialized bus.Topic = "hopspace.initialized"

	DepsInstalled bus.Topic = "deps.installed"
)

// Source identifies the emitter in Event.Source.
const Source = "git-hop"

// WorktreeEvent is the payload for worktree lifecycle events.
type WorktreeEvent struct {
	Path          string // Worktree directory path.
	Branch        string // Branch name.
	HopspacePath  string // Hopspace root path.
	RepoPath      string // Hub/bare-repo path.
}

// EnvEvent is the payload for environment lifecycle events.
type EnvEvent struct {
	Action string // "start" or "stop".
	Root   string // Git root of the worktree.
	Branch string // Branch name.
}

// HopspaceEvent is the payload for hopspace initialization.
type HopspaceEvent struct {
	Path   string // Path where hopspace was initialized.
	Org    string
	Repo   string
}

// DepsEvent is the payload emitted after dependency installation.
type DepsEvent struct {
	WorktreePath string
	Branch       string
}
