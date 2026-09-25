package cli_test

import (
	"errors"
	"os/exec"
	"path/filepath"
	"testing"

	"hop.top/git/internal/cli"
)

func gitConfigCmd(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// An org/repo shorthand expands with --git-domain when given, else with
// hop.gitDomain resolved like any hop.* setting: the hub's own value over
// --global, and only --global outside a hub.
func TestShorthandDomain_ResolvesHopGitDomainPerHub(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	gitConfigCmd(t, "config", "--global", "hop.gitDomain", "global.example")
	hub := t.TempDir()
	gitConfigCmd(t, "init", "-q", "--bare", hub)
	gitConfigCmd(t, "-C", hub, "config", "hop.gitDomain", "hub.example")

	if got := cli.ShorthandDomain("", hub, nil); got != "hub.example" {
		t.Errorf("in a hub: %q, want the hub's hub.example", got)
	}
	if got := cli.ShorthandDomain("", hub, errors.New("not in a hub")); got != "global.example" {
		t.Errorf("outside a hub: %q, want the --global global.example", got)
	}
	if got := cli.ShorthandDomain("flag.example", hub, nil); got != "flag.example" {
		t.Errorf("with --git-domain: %q, want flag.example", got)
	}
	if got, want := cli.ResolveArg("acme/widgets", cli.ShorthandDomain("", hub, nil), nil), "git@hub.example:acme/widgets.git"; got != want {
		t.Errorf("ResolveArg = %q, want %q", got, want)
	}
}
