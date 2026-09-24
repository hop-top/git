package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"hop.top/kit/go/runtime/bus"

	"hop.top/git/internal/config"
	"hop.top/git/internal/events"
)

// fileGitConfig returns a GitConfig backed by a private config file, so the
// tests exercise real `git config` parsing (including --type=path tilde
// expansion) without touching the developer's settings.
func fileGitConfig(t *testing.T, kv map[string]string) *config.GitConfig {
	t.Helper()
	file := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for k, v := range kv {
		if out, err := exec.Command("git", "config", "--file", file, k, v).CombinedOutput(); err != nil {
			t.Fatalf("git config %s: %v\n%s", k, err, out)
		}
	}
	return &config.GitConfig{RunCmd: func(args ...string) (string, error) {
		full := append([]string{args[0], "--file", file}, args[1:]...)
		out, err := exec.Command("git", full...).Output()
		return strings.TrimSpace(string(out)), err
	}}
}

func noEnv(string) string { return "" }

func TestResolveEventSinkPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	cases := []struct {
		name    string
		kv      map[string]string
		env     map[string]string
		want    string
		wantErr bool
	}{
		{name: "unset", kv: nil, want: ""},
		{name: "none", kv: map[string]string{config.KeyEventsSink: "none"}, want: ""},
		{name: "jsonl explicit path",
			kv:   map[string]string{config.KeyEventsSink: "jsonl", config.KeyEventsPath: "/var/tmp/hop.jsonl"},
			want: "/var/tmp/hop.jsonl"},
		{name: "jsonl case-insensitive", kv: map[string]string{config.KeyEventsSink: "JSONL", config.KeyEventsPath: "/x.jsonl"},
			want: "/x.jsonl"},
		{name: "jsonl tilde path",
			kv:   map[string]string{config.KeyEventsSink: "jsonl", config.KeyEventsPath: "~/ev.jsonl"},
			want: filepath.Join(home, "ev.jsonl")},
		{name: "jsonl default path",
			kv:   map[string]string{config.KeyEventsSink: "jsonl"},
			want: filepath.Join(state, "git-hop", "events.jsonl")},
		{name: "unknown kind", kv: map[string]string{config.KeyEventsSink: "socket"}, wantErr: true},
		{name: "kit env sink wins",
			kv:   map[string]string{config.KeyEventsSink: "jsonl", config.KeyEventsPath: "/x.jsonl"},
			env:  map[string]string{bus.EnvSinkKind: "jsonl"},
			want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			getenv := noEnv
			if tc.env != nil {
				getenv = func(k string) string { return tc.env[k] }
			}
			got, err := resolveEventSinkPath(fileGitConfig(t, tc.kv), getenv)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got path %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("path = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCommandIsDryRun(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	root.PersistentFlags().Bool("dry-run", false, "")
	sub := &cobra.Command{Use: "sub", Run: func(*cobra.Command, []string) {}}
	local := &cobra.Command{Use: "local", Run: func(*cobra.Command, []string) {}}
	local.Flags().Bool("dry-run", false, "")
	root.AddCommand(sub, local)

	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"sub"}, false},
		{[]string{"sub", "--dry-run"}, true},
		{[]string{"local"}, false},
		{[]string{"local", "--dry-run"}, true},
	} {
		root.SetArgs(tc.args)
		c, err := root.ExecuteC()
		if err != nil {
			t.Fatal(err)
		}
		if got := commandIsDryRun(c); got != tc.want {
			t.Errorf("%v: commandIsDryRun = %v, want %v", tc.args, got, tc.want)
		}
		_ = c.Flags().Set("dry-run", "false")
	}
}

// The tee must keep in-process delivery intact (the deps installer is a
// sync subscriber) while also writing the sink.
func TestWithEventSink_TeesToFileAndSubscribers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	b := withEventSink(bus.New(), path, nil)
	defer func() { _ = b.Close(context.Background()) }()

	delivered := 0
	b.Subscribe(string(events.WorktreeCreated), func(context.Context, bus.Event) error {
		delivered++
		return nil
	})
	if err := b.Publish(context.Background(), bus.NewEvent(events.WorktreeCreated, events.Source,
		events.WorktreeEvent{Branch: "a"})); err != nil {
		t.Fatal(err)
	}
	if delivered != 1 {
		t.Errorf("subscriber deliveries = %d, want 1", delivered)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("sink file: %v", err)
	}
	if !strings.Contains(string(data), `"topic":"git.runtime.worktree.created"`) {
		t.Errorf("sink file = %s", data)
	}
}

// A failing sink reports through onErr and never fails the publish.
func TestWithEventSink_FailureIsReportedNotReturned(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var reported []error
	b := withEventSink(bus.New(), filepath.Join(blocker, "events.jsonl"), func(err error) {
		reported = append(reported, err)
	})
	defer func() { _ = b.Close(context.Background()) }()

	if err := b.Publish(context.Background(), bus.NewEvent(events.WorktreeCreated, events.Source,
		events.WorktreeEvent{})); err != nil {
		t.Fatalf("Publish returned sink error: %v", err)
	}
	if len(reported) != 1 {
		t.Fatalf("reported %d errors, want 1", len(reported))
	}
}

// attachEventSinks end to end against real git config. The dry-run case is
// the guard itself: commands also skip publishing under --dry-run, so only
// this test catches a sink attached during a preview.
func TestAttachEventSinks(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		wantFile bool
	}{
		{"attached", []string{"sub"}, true},
		{"dry-run skips", []string{"sub", "--dry-run"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			sinkPath := filepath.Join(dir, "events.jsonl")
			gitCfg := filepath.Join(dir, "gitconfig")
			t.Setenv("GIT_CONFIG_GLOBAL", gitCfg)
			t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(dir, "absent"))
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			t.Setenv(bus.EnvSinkKind, "")
			for k, v := range map[string]string{config.KeyEventsSink: "jsonl", config.KeyEventsPath: sinkPath} {
				if out, err := exec.Command("git", "config", "--file", gitCfg, k, v).CombinedOutput(); err != nil {
					t.Fatalf("%v\n%s", err, out)
				}
			}

			prev := EventBus
			EventBus = bus.New()
			t.Cleanup(func() {
				_ = EventBus.Close(context.Background())
				EventBus = prev
			})

			root := &cobra.Command{Use: "root"}
			root.PersistentFlags().Bool("dry-run", false, "")
			sub := &cobra.Command{Use: "sub", Run: func(c *cobra.Command, _ []string) { attachEventSinks(c) }}
			root.AddCommand(sub)
			root.SetArgs(tc.args)
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}

			if err := EventBus.Publish(context.Background(), bus.NewEvent(events.WorktreeCreated, events.Source,
				events.WorktreeEvent{Branch: "x"})); err != nil {
				t.Fatal(err)
			}
			_, err := os.Stat(sinkPath)
			if got := err == nil; got != tc.wantFile {
				t.Errorf("sink file exists = %v, want %v", got, tc.wantFile)
			}
		})
	}
}
