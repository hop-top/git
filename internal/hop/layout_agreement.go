package hop

import (
	"fmt"
	"path/filepath"
	"strings"

	"hop.top/git/internal/config"
)

// hop.dataLayout resolves per repository (ResolveDataLayout): a hub's own
// value overrides --global. Two hubs of one repository can so put its
// data-home hopspace, and the hopspace hooks dir in it, at two paths.
// Moving that data is then ambiguous, and a hub that still resolves the
// old path writes there after a move. doctor --fix moves nothing while
// the hubs disagree, and a hook mirror warns; both name the hubs and say
// how to align them (LayoutAlignmentHint).

// HubLayout is hop.dataLayout as one hub of a repository resolves it,
// and the hopspace path that puts the repository at.
type HubLayout struct {
	Hub     string
	Setting DataLayoutSetting
	Path    string
}

// ResolveHubLayouts resolves hop.dataLayout for each of hubs, in order,
// and where it puts ref's hopspace under dataHome. It runs git config
// for every hub: call it outside any lock.
func ResolveHubLayouts(dataHome string, ref RepoRef, hubs []string) []HubLayout {
	layouts := make([]HubLayout, 0, len(hubs))
	for _, hub := range hubs {
		s := ResolveDataLayout(hub)
		layouts = append(layouts, HubLayout{
			Hub:     hub,
			Setting: s,
			Path:    filepath.Clean(hopspacePathIn(dataHome, s.Layout, ref.In(hub))),
		})
	}
	return layouts
}

// AgreedHopspace returns the hopspace path every one of layouts
// resolves; ok is false when they resolve several, or there are none.
func AgreedHopspace(layouts []HubLayout) (path string, ok bool) {
	for i, l := range layouts {
		if i > 0 && l.Path != path {
			return "", false
		}
		path = l.Path
	}
	return path, len(layouts) > 0
}

// LayoutSplitSummary names, for each path layouts resolve, the hubs that
// resolve it: "/a, /b -> /data/x; /c -> /data/y".
func LayoutSplitSummary(layouts []HubLayout) string {
	var paths []string
	byPath := map[string][]string{}
	for _, l := range layouts {
		if byPath[l.Path] == nil {
			paths = append(paths, l.Path)
		}
		byPath[l.Path] = append(byPath[l.Path], l.Hub)
	}
	parts := make([]string, 0, len(paths))
	for _, p := range paths {
		parts = append(parts, fmt.Sprintf("%s -> %s", strings.Join(byPath[p], ", "), p))
	}
	return strings.Join(parts, "; ")
}

// LayoutAlignmentHint is the advice for hubs that resolve hop.dataLayout
// differently: the value each hub has in effect and where it comes from,
// and the git config commands that give them one value.
func LayoutAlignmentHint(layouts []HubLayout) string {
	key := config.KeyDataLayout
	var b strings.Builder
	fmt.Fprintf(&b, "%s as each hub resolves it:\n", key)
	var unset []string
	for _, l := range layouts {
		fmt.Fprintf(&b, "  %s: %s\n", l.Hub, describeLayoutSetting(l.Setting))
		if cmd := unsetOverrideCommand(l); cmd != "" {
			unset = append(unset, cmd)
		}
	}
	b.WriteString("Give them one value: set it for every repository with\n")
	fmt.Fprintf(&b, "  git config --global %s <layout>\n", key)
	if len(unset) > 0 {
		b.WriteString("and drop the hub values that override it:\n")
		for _, cmd := range unset {
			fmt.Fprintf(&b, "  %s\n", cmd)
		}
	}
	fmt.Fprintf(&b, "or set the same value in each hub: git -C <hub> config %s <layout>", key)
	return b.String()
}

// describeLayoutSetting says which layout s puts in effect and why:
// "{org}/{repo} (local config)", "{org}/{repo} (default)", or for an
// unusable value, what it is and what is used instead.
func describeLayoutSetting(s DataLayoutSetting) string {
	switch {
	case s.Err != nil:
		return fmt.Sprintf("%q (%s config) is invalid; using %s", s.Raw, s.Scope, s.Layout)
	case s.Scope == "":
		return s.Layout + " (default)"
	default:
		return fmt.Sprintf("%s (%s config)", s.Layout, s.Scope)
	}
}

// unsetOverrideCommand returns the command that drops l's hub's own
// hop.dataLayout, "" when the value it has in effect is not its own.
func unsetOverrideCommand(l HubLayout) string {
	switch l.Setting.Scope {
	case "local":
		return fmt.Sprintf("git -C %s config --unset %s", l.Hub, config.KeyDataLayout)
	case "worktree":
		return fmt.Sprintf("git -C %s config --worktree --unset %s", l.Hub, config.KeyDataLayout)
	}
	return ""
}
