package config_test

import (
	"strings"
	"testing"
)

// The warning fires only for the extension on at version 0 (or unset);
// the other rows are the inverse guard.
func TestWorktreeConfigAtFormatV0(t *testing.T) {
	for _, tc := range []struct {
		name string
		kv   map[string]string
		want bool
	}{
		{"extension on, version 0", map[string]string{"extensions.worktreeConfig": "true", "core.repositoryformatversion": "0"}, true},
		{"extension on, version 1", map[string]string{"extensions.worktreeConfig": "true", "core.repositoryformatversion": "1"}, false},
		{"extension off, version 0", map[string]string{"extensions.worktreeConfig": "false", "core.repositoryformatversion": "0"}, false},
		{"no extension, version 0", map[string]string{"core.repositoryformatversion": "0"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hub := newHubRepo(t)
			setLocal(t, hub, tc.kv)
			got, err := hubScope(t, hub).WorktreeConfigAtFormatV0()
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("WorktreeConfigAtFormatV0() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSetRepositoryFormatVersion1(t *testing.T) {
	hub := newHubRepo(t)
	setLocal(t, hub, map[string]string{"extensions.worktreeConfig": "true", "core.repositoryformatversion": "0"})
	s := hubScope(t, hub)

	if err := s.SetRepositoryFormatVersion1(); err != nil {
		t.Fatal(err)
	}
	if out, _ := gitIn(t, hub, "config", "--local", "core.repositoryformatversion"); strings.TrimSpace(out) != "1" {
		t.Errorf("core.repositoryformatversion = %q, want 1", out)
	}
	if stale, err := s.WorktreeConfigAtFormatV0(); err != nil || stale {
		t.Errorf("after the fix WorktreeConfigAtFormatV0() = %v, %v; want false", stale, err)
	}
}
