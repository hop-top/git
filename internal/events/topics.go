// Package events defines topic constants and payload structs for the
// git-hop event bus.
package events

import "hop.top/kit/go/runtime/bus"

// Topic constants follow kit's 4-segment grammar:
// [source].[category].[object].[action] with a past-tense action.
const (
	WorktreeCreated  bus.Topic = "git.runtime.worktree.created"
	WorktreeRemoved  bus.Topic = "git.runtime.worktree.removed"
	WorktreeMerged   bus.Topic = "git.runtime.worktree.merged"
	WorktreeMoved    bus.Topic = "git.runtime.worktree.moved"
	WorktreeSwitched bus.Topic = "git.runtime.worktree.switched"

	EnvStarted bus.Topic = "git.runtime.env.started"
	EnvStopped bus.Topic = "git.runtime.env.stopped"

	HopspaceInitialized bus.Topic = "git.runtime.hopspace.initialized"

	DepsInstalled bus.Topic = "git.runtime.deps.installed"
)

// Source identifies the emitter in Event.Source.
const Source = "git-hop"

// Payload structs carry snake_case JSON tags: kit's bus contract requires
// lowercase wire keys, and sinks and cross-process consumers see only the
// JSON form.

// WorktreeEvent is the payload for worktree lifecycle events.
type WorktreeEvent struct {
	Path         string `json:"path"`          // Worktree directory path.
	Branch       string `json:"branch"`        // Branch name.
	HopspacePath string `json:"hopspace_path"` // Hopspace root path.
	RepoPath     string `json:"repo_path"`     // Hub/bare-repo path.
}

// EnvEvent is the payload for environment lifecycle events.
type EnvEvent struct {
	Action string `json:"action"` // "start" or "stop".
	Root   string `json:"root"`   // Git root of the worktree.
	Branch string `json:"branch"` // Branch name.
}

// HopspaceEvent is the payload for hopspace initialization.
type HopspaceEvent struct {
	Path string `json:"path"` // Path where hopspace was initialized.
	Org  string `json:"org"`
	Repo string `json:"repo"`
}

// DepsEvent is the payload emitted after dependency installation.
type DepsEvent struct {
	WorktreePath string `json:"worktree_path"`
	Branch       string `json:"branch"`
}
