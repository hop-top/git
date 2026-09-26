package hop_test

import (
	"errors"
	"strings"
	"testing"

	"hop.top/git/internal/hop"
)

func TestAgreedHopspace(t *testing.T) {
	at := func(paths ...string) []hop.HubLayout {
		var ls []hop.HubLayout
		for _, p := range paths {
			ls = append(ls, hop.HubLayout{Hub: "/hub" + p, Path: p})
		}
		return ls
	}
	if _, ok := hop.AgreedHopspace(nil); ok {
		t.Error("no hubs agree on nothing")
	}
	if p, ok := hop.AgreedHopspace(at("/d/x", "/d/x")); !ok || p != "/d/x" {
		t.Errorf("AgreedHopspace = %q, %v; want /d/x, true", p, ok)
	}
	if _, ok := hop.AgreedHopspace(at("/d/x", "/d/x", "/d/y")); ok {
		t.Error("hubs resolving two paths must not agree")
	}
}

// The hint names each hub's value and where it comes from, and the
// command that drops each hub's own value, at the scope it is set in.
func TestLayoutAlignmentHint(t *testing.T) {
	layouts := []hop.HubLayout{
		{Hub: "/a", Setting: hop.DataLayoutSetting{Layout: "{host}/{org}/{repo}", Raw: "{host}/{org}/{repo}", Scope: "local"}},
		{Hub: "/b", Setting: hop.DataLayoutSetting{Layout: "{org}/{repo}"}},
		{Hub: "/c", Setting: hop.DataLayoutSetting{Layout: "{host}/{org}/{repo}", Raw: "{host}/{org}/{repo}", Scope: "worktree"}},
		{Hub: "/d", Setting: hop.DataLayoutSetting{Layout: "{org}/{repo}", Raw: "{repo}", Scope: "local", Err: errors.New("must contain {org}")}},
		{Hub: "/e", Setting: hop.DataLayoutSetting{Layout: "{org}/{repo}", Raw: "{org}/{repo}", Scope: "global"}},
	}

	got := hop.LayoutAlignmentHint(layouts)

	for _, want := range []string{
		"  /a: {host}/{org}/{repo} (local config)\n",
		"  /b: {org}/{repo} (default)\n",
		"  /c: {host}/{org}/{repo} (worktree config)\n",
		"  /d: \"{repo}\" (local config) is invalid; using {org}/{repo}\n",
		"  /e: {org}/{repo} (global config)\n",
		"  git config --global hop.dataLayout <layout>\n",
		"  git -C /a config --unset hop.dataLayout\n",
		"  git -C /c config --worktree --unset hop.dataLayout\n",
		"  git -C /d config --unset hop.dataLayout\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("hint lacks %q:\n%s", want, got)
		}
	}
	for _, hub := range []string{"/b", "/e"} {
		if strings.Contains(got, "git -C "+hub+" config --unset") {
			t.Errorf("hint drops %s's value, which is not its own:\n%s", hub, got)
		}
	}
}
