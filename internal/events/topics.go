// Package events defines topic constants and payload structs for the
// git-hop event bus.
package events

import "hop.top/kit/go/runtime/bus"

// Topic constants follow kit's 4-segment grammar:
// [source].[category].[object].[action] with a past-tense action.
const (
	WorktreeCreated bus.Topic = "git.runtime.worktree.created"
	WorktreeRemoved bus.Topic = "git.runtime.worktree.removed"
	WorktreeMerged  bus.Topic = "git.runtime.worktree.merged"
	WorktreeMoved   bus.Topic = "git.runtime.worktree.moved"

	EnvStarted bus.Topic = "git.runtime.env.started"
	EnvStopped bus.Topic = "git.runtime.env.stopped"

	HopspaceInitialized bus.Topic = "git.runtime.hopspace.initialized"

	DepsInstalled bus.Topic = "git.runtime.deps.installed"
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
