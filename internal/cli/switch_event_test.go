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
// branch switch. HopspacePath is the hopspace the hub resolves to, the
// same value created/moved/removed report: the hub itself by default, the
// data-home hopspace for a hub marked global. It must never be empty.
func TestPublishWorktreeSwitched(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("GIT_HOP_DATA_HOME", dataHome)
	global := filepath.Join(dataHome, "ideacrafterslabs", "git")

	for _, tc := range []struct {
		name         string
		mode         string
		wantHopspace string
	}{
		{"default hub is its own hopspace", "", "/hubs/git"},
		{"hub marked global names the data-home hopspace", config.RepoModeGlobal, global},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := publishAndCollect(t, tc.mode)

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
			want := events.WorktreeEvent{
				Path:         "/hubs/git/hops/feat/switch",
				Branch:       "feat/switch",
				HopspacePath: tc.wantHopspace,
				RepoPath:     "/hubs/git",
			}
			if payload != want {
				t.Errorf("payload = %+v, want %+v", payload, want)
			}
			if ev.Timestamp.IsZero() {
				t.Error("Timestamp is zero")
			}
			if time.Since(ev.Timestamp) > time.Minute {
				t.Errorf("Timestamp = %v, want recent", ev.Timestamp)
			}
		})
	}
}

func publishAndCollect(t *testing.T, mode string) []bus.Event {
	t.Helper()
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
				Mode:          mode,
			},
		},
	}
	publishWorktreeSwitched(b, hub, "/hubs/git", "feat/switch", "/hubs/git/hops/feat/switch")

	mu.Lock()
	defer mu.Unlock()
	return append([]bus.Event(nil), received...)
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
