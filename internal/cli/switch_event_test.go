package cli

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"hop.top/kit/go/runtime/bus"

	"hop.top/git/internal/config"
	"hop.top/git/internal/events"
	"hop.top/git/internal/hop"
)

// TestPublishWorktreeSwitched pins the payload emitted after a successful
// branch switch. HopspacePath must be the pure org/repo path computation, not
// an empty string: a switch resolves it from hub config without loading the
// hopspace.
func TestPublishWorktreeSwitched(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("GIT_HOP_DATA_HOME", dataHome)

	b := bus.New()
	defer func() { _ = b.Close(context.Background()) }()

	var (
		mu       sync.Mutex
		received []bus.Event
	)
	b.Subscribe(string(events.WorktreeSwitched), func(_ context.Context, e bus.Event) error {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, e)
		return nil
	})

	hub := &hop.Hub{
		Path: "/hubs/git",
		Config: &config.HubConfig{
			Repo: config.RepoConfig{
				Org:           "ideacrafterslabs",
				Repo:          "git",
				DefaultBranch: "main",
			},
		},
	}

	publishWorktreeSwitched(b, hub, "/hubs/git", "feat/switch", "/hubs/git/hops/feat/switch")

	mu.Lock()
	got := append([]bus.Event(nil), received...)
	mu.Unlock()

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 event on %s, got %d", events.WorktreeSwitched, len(got))
	}

	ev := got[0]
	if ev.Topic != events.WorktreeSwitched {
		t.Errorf("Topic = %q, want %q", ev.Topic, events.WorktreeSwitched)
	}
	if ev.Source != events.Source {
		t.Errorf("Source = %q, want %q", ev.Source, events.Source)
	}

	payload, ok := ev.Payload.(events.WorktreeEvent)
	if !ok {
		t.Fatalf("Payload type = %T, want events.WorktreeEvent", ev.Payload)
	}

	wantHopspace := filepath.Join(dataHome, "ideacrafterslabs", "git")
	want := events.WorktreeEvent{
		Path:         "/hubs/git/hops/feat/switch",
		Branch:       "feat/switch",
		HopspacePath: wantHopspace,
		RepoPath:     "/hubs/git",
	}
	if payload != want {
		t.Errorf("payload = %+v, want %+v", payload, want)
	}
	if payload.HopspacePath == "" {
		t.Error("HopspacePath is empty; switch path must compute it from hub org/repo")
	}
	if ev.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
	if time.Since(ev.Timestamp) > time.Minute {
		t.Errorf("Timestamp = %v, want recent", ev.Timestamp)
	}
}

// TestPublishWorktreeSwitched_NilBusIsNoop guards the independence property:
// an unavailable bus must never panic or otherwise fail the switch.
func TestPublishWorktreeSwitched_NilBusIsNoop(t *testing.T) {
	hub := &hop.Hub{
		Config: &config.HubConfig{
			Repo: config.RepoConfig{Org: "o", Repo: "r"},
		},
	}
	publishWorktreeSwitched(nil, hub, "/hub", "b", "/hub/b")
}
