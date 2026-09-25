package hop

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"hop.top/git/internal/config"
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
	return RepoRef{Host: ParseHostFromURL(uri), Org: org, Repo: repo}
}

// RepoRefFor returns the RepoRef of the repository a hub or hopspace
// config describes, with hop.dataLayout read from the hub at hubPath.
func RepoRefFor(hubPath string, repo config.RepoConfig) RepoRef {
	return NewRepoRef(repo.URI, repo.Org, repo.Repo).In(hubPath)
}

// RepoRefFromID returns the RepoRef of a 3-part repo ID ("host/org/repo"),
// with the host taken from uri rather than the ID: repo IDs carry a fixed
// host whatever the origin. ok is false when the ID has fewer than three
// parts.
func RepoRefFromID(repoID, uri string) (ref RepoRef, ok bool) {
	parts := strings.Split(repoID, "/")
	if len(parts) < 3 || parts[1] == "" || parts[2] == "" {
		return RepoRef{}, false
	}
	return NewRepoRef(uri, parts[1], parts[2]), true
}

// ParseHostFromURL returns the lowercased host of a git remote URL, without
// user or port: "gitlab.example.com" for https://gitlab.example.com/a/b.git,
// ssh://git@gitlab.example.com:2222/a/b.git and git@gitlab.example.com:a/b.git
// alike. Local remotes (file:// URLs and paths) have no host and yield "".
func ParseHostFromURL(uri string) string {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return ""
	}
	if strings.Contains(uri, "://") {
		u, err := url.Parse(uri)
		if err != nil || u.Scheme == "file" {
			return ""
		}
		return strings.ToLower(u.Hostname())
	}
	// scp-like syntax, [user@]host:path. git reads it as such only when
	// the colon comes before any slash; otherwise it is a local path.
	colon := strings.Index(uri, ":")
	if colon <= 0 || strings.Contains(uri[:colon], "/") {
		return ""
	}
	host := uri[:colon]
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}
	return strings.ToLower(strings.Trim(host, "[]"))
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
// the way git resolves any setting: a repository (local or worktree)
// value overrides --global, and `git -c` overrides both. With dir empty,
// or not a directory git can enter, there is no repository: only the
// system, --global and `git -c` values count, whatever repository the
// process runs in.
func ResolveDataLayout(dir string) DataLayoutSetting {
	def := config.Default(config.KeyDataLayout)
	vals, ok := scopedDataLayout(dir)
	if !ok || len(vals) == 0 {
		return DataLayoutSetting{Layout: def}
	}
	eff := vals[len(vals)-1]
	s := DataLayoutSetting{Layout: eff.Value, Raw: eff.Value, Scope: eff.Scope}
	if s.Err = ValidateDataLayout(eff.Value); s.Err == nil {
		return s
	}
	s.Layout = def
	if eff.Scope != "global" {
		for i := len(vals) - 1; i >= 0; i-- {
			if vals[i].Scope == "global" {
				if ValidateDataLayout(vals[i].Value) == nil {
					s.Layout = vals[i].Value
				}
				break
			}
		}
	}
	return s
}

// scopedDataLayout lists the hop.dataLayout values that apply to the
// repository at dir, lowest precedence first. ok is false when git config
// cannot be read at all.
func scopedDataLayout(dir string) (vals []config.ScopedValue, ok bool) {
	if dir != "" {
		if vals, err := config.NewGitConfigIn(dir).GetAllScoped(config.KeyDataLayout); err == nil {
			return vals, true
		}
	}
	all, err := config.NewGitConfig().GetAllScoped(config.KeyDataLayout)
	if err != nil {
		return nil, false
	}
	for _, v := range all {
		if v.Scope != "local" && v.Scope != "worktree" {
			vals = append(vals, v)
		}
	}
	return vals, true
}

// DataLayout returns the hop.dataLayout template in effect outside any
// repository: the --global (or `git -c`) value, else the default.
func DataLayout() string {
	return ResolveDataLayout("").Layout
}

// ExpandDataLayout substitutes ref into layout. An empty ref.Host becomes
// hop.gitDomain, the host org/repo shorthands expand to.
func ExpandDataLayout(layout string, ref RepoRef) string {
	host := ref.Host
	if host == "" && strings.Contains(layout, layoutHost) {
		host = config.NewGlobalGitConfig().GetStringOrDefault(config.KeyGitDomain)
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
	return filepath.Join(dataHome, ExpandDataLayout(ResolveDataLayout(ref.Dir).Layout, ref))
}

// HopspaceHooksDir returns the directory hopspace-level hooks of ref are
// mirrored to and looked up in first.
func HopspaceHooksDir(ref RepoRef) string {
	return filepath.Join(GetHopspacePath(GetGitHopDataHome(), ref), "hooks")
}

// LegacyHooksDir returns where releases before hop.dataLayout kept a
// repository's hopspace hooks: <data>/<host>/<org>/<repo>/hooks, the host
// being the fixed one of the repo ID (github.com), whatever the origin.
// Hook lookup still reads it after HopspaceHooksDir; doctor --fix moves
// it. "" when repoID has fewer than three parts.
func LegacyHooksDir(repoID string) string {
	parts := strings.Split(repoID, "/")
	if len(parts) < 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return ""
	}
	return filepath.Join(GetGitHopDataHome(), parts[0], parts[1], parts[2], "hooks")
}
