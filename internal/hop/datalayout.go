package hop

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"hop.top/git/internal/config"
	"hop.top/git/internal/repoid"
)

// RepoRef names a repository's place in the data home. Host is the host
// of the repository's origin URL; it only matters to a data layout that
// uses {host}, and an empty Host stands for hop.gitDomain. Dir is a
// directory of the repository (its hub, or one of its worktrees) whose
// git config decides hop.dataLayout; empty when there is no repository
// yet, as before a --global clone, in which case only the --global and
// `git -c` values count.
type RepoRef struct {
	Host string
	Org  string
	Repo string
	Dir  string
}

// In returns ref with hop.dataLayout read from the repository at dir.
func (r RepoRef) In(dir string) RepoRef {
	r.Dir = dir
	return r
}

// NewRepoRef returns the RepoRef of the repository org/repo cloned from
// uri, taking the host from uri.
func NewRepoRef(uri, org, repo string) RepoRef {
	return RepoRef{Host: repoid.Host(uri), Org: org, Repo: repo}
}

// RepoRefFor returns the RepoRef of the repository a hub or hopspace
// config describes, with hop.dataLayout read from the hub at hubPath.
func RepoRefFor(hubPath string, repo config.RepoConfig) RepoRef {
	return NewRepoRef(repo.URI, repo.Org, repo.Repo).In(hubPath)
}

// RepoRefFromID returns the RepoRef of a repo ID ("host/org/repo"), with
// the host taken from uri as NewRepoRef does: for a local origin the ID
// says hop.gitDomain, which the layout expands an empty Host to anyway.
// ok is false when repoID is not a 3-part ID.
func RepoRefFromID(repoID, uri string) (ref RepoRef, ok bool) {
	_, org, repo, ok := repoid.Split(repoID)
	if !ok {
		return RepoRef{}, false
	}
	return NewRepoRef(uri, org, repo), true
}

// Data layout placeholders.
const (
	layoutHost = "{host}"
	layoutOrg  = "{org}"
	layoutRepo = "{repo}"
)

var layoutPlaceholder = regexp.MustCompile(`\{[^{}]*\}`)

// ValidateDataLayout reports whether layout is a usable hop.dataLayout: a
// relative path that names {org} and {repo} (so no two repositories share
// a directory), may name {host}, and uses no other placeholder and no
// "." or ".." segment.
func ValidateDataLayout(layout string) error {
	if strings.TrimSpace(layout) == "" {
		return fmt.Errorf("empty")
	}
	if filepath.IsAbs(layout) || strings.HasPrefix(layout, "/") || strings.HasPrefix(layout, "\\") {
		return fmt.Errorf("must be relative to the data home")
	}
	for _, p := range layoutPlaceholder.FindAllString(layout, -1) {
		if p != layoutHost && p != layoutOrg && p != layoutRepo {
			return fmt.Errorf("unknown placeholder %s (want %s, %s, %s)", p, layoutHost, layoutOrg, layoutRepo)
		}
	}
	for _, need := range []string{layoutOrg, layoutRepo} {
		if !strings.Contains(layout, need) {
			return fmt.Errorf("must contain %s", need)
		}
	}
	for _, seg := range strings.FieldsFunc(layout, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == "." || seg == ".." {
			return fmt.Errorf("must not contain %q segments", seg)
		}
	}
	return nil
}

// DataLayoutSetting is hop.dataLayout as resolved for one repository.
type DataLayoutSetting struct {
	// Layout is the template in effect.
	Layout string
	// Raw is the configured value git config gives precedence, "" when
	// the key is set nowhere.
	Raw string
	// Scope is the git config scope Raw came from ("global", "local",
	// "worktree", "command" for `git -c`, "system"); "" when unset.
	Scope string
	// Err says why Raw is unusable, in which case Layout is the --global
	// value, or the default when that is unset or unusable too.
	Err error
}

// ResolveDataLayout resolves hop.dataLayout for the repository at dir
// the way git resolves any setting (config.ResolveScoped): a repository
// (local or worktree) value overrides --global, and `git -c` overrides
// both. With dir empty, or not a directory git can enter, there is no
// repository: only the system, --global and `git -c` values count,
// whatever repository the process runs in.
func ResolveDataLayout(dir string) DataLayoutSetting {
	s := config.ResolveScoped(dir, config.KeyDataLayout, ValidateDataLayout)
	return DataLayoutSetting{Layout: s.Value, Raw: s.Raw, Scope: s.Scope, Err: s.Err}
}

// DataLayout returns the hop.dataLayout template in effect outside any
// repository: the --global (or `git -c`) value, else the default.
func DataLayout() string {
	return ResolveDataLayout("").Layout
}

// ExpandDataLayout substitutes ref into layout. An empty ref.Host becomes
// hop.gitDomain as resolved for ref.Dir, the host org/repo shorthands
// expand to.
func ExpandDataLayout(layout string, ref RepoRef) string {
	host := ref.Host
	if host == "" && strings.Contains(layout, layoutHost) {
		host = repoid.GitDomainIn(ref.Dir)
	}
	r := strings.NewReplacer(layoutHost, host, layoutOrg, ref.Org, layoutRepo, ref.Repo)
	return filepath.FromSlash(r.Replace(layout))
}

// GetHopspacePath returns the data-home location for a repo's hopspace:
// dataHome joined with hop.dataLayout, as resolved for ref.Dir, expanded
// for ref. Every per-repo path under the data home (hopspace config,
// ports, volumes, deps store, hooks) derives from it. It is where a
// --global clone creates the hopspace; to find the hopspace an existing
// hub uses, call ResolveHopspacePath.
func GetHopspacePath(dataHome string, ref RepoRef) string {
	return hopspacePathIn(dataHome, ResolveDataLayout(ref.Dir).Layout, ref)
}

// hopspacePathIn is GetHopspacePath with hop.dataLayout already resolved.
func hopspacePathIn(dataHome, layout string, ref RepoRef) string {
	return filepath.Join(dataHome, ExpandDataLayout(layout, ref))
}

// HopspaceHooksDir returns the directory hopspace-level hooks of ref are
// mirrored to and looked up in first.
func HopspaceHooksDir(ref RepoRef) string {
	return filepath.Join(GetHopspacePath(GetGitHopDataHome(), ref), "hooks")
}

// LegacyHooksHost is the host every repo ID had before IDs carried the
// origin's: releases before hop.dataLayout mirrored hooks under it.
const LegacyHooksHost = "github.com"

// LegacyHooksDir returns where releases before hop.dataLayout kept a
// repository's hopspace hooks: <data>/github.com/<org>/<repo>/hooks,
// whatever the origin, since their repo IDs always said github.com. Hook
// lookup still reads it after HopspaceHooksDir; doctor --fix moves it.
// "" when repoID is not a 3-part ID.
func LegacyHooksDir(repoID string) string {
	_, org, repo, ok := repoid.Split(repoID)
	if !ok {
		return ""
	}
	return filepath.Join(GetGitHopDataHome(), LegacyHooksHost, org, repo, "hooks")
}
