// Package repoid builds the repository ID git-hop keys state by and
// passes to hooks as GIT_HOP_REPO_ID: "<host>/<org>/<repo>", the host
// being the one of the repository's origin URL.
//
// Every repo ID is built by New (or For); nothing else formats one.
package repoid

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"hop.top/git/internal/config"
)

// Host returns the lowercased host of a git remote URL, without user or
// port: "gitlab.example.com" for https://gitlab.example.com/a/b.git,
// ssh://git@gitlab.example.com:2222/a/b.git and git@gitlab.example.com:a/b.git
// alike. Local remotes (file:// URLs and paths) have no host and yield "".
func Host(uri string) string {
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

// ValidateGitDomain reports why v cannot be hop.gitDomain: a host name,
// so not empty and without a "/" or whitespace.
func ValidateGitDomain(v string) error {
	switch {
	case strings.TrimSpace(v) == "":
		return errors.New("empty")
	case strings.ContainsAny(v, "/ \t\n"):
		return fmt.Errorf("%q is not a host name", v)
	}
	return nil
}

// GitDomainIn returns hop.gitDomain as resolved for the repository at
// dir (config.ResolveScoped, the resolver hop.dataLayout uses): the
// repository's own value over --global, `git -c` over both, the default
// (github.com) when unset. With dir empty there is no repository and only
// --global and `git -c` count. It is the host org/repo shorthands expand
// to, and the host of a repository whose origin has none.
func GitDomainIn(dir string) string {
	return config.ResolveScoped(dir, config.KeyGitDomain, ValidateGitDomain).Value
}

// GitDomain is GitDomainIn outside any repository.
func GitDomain() string {
	return GitDomainIn("")
}

// New returns the ID of the repository org/repo whose origin is uri,
// before it has a hub: "<host>/<org>/<repo>", with the host taken from
// uri, or hop.gitDomain (GitDomain) when uri has none (a local path, a
// file:// URL, no origin at all). It returns "" when org or repo is
// empty: a partial ID looks like a real one to a hook while naming
// nothing.
func New(uri, org, repo string) string {
	return NewIn("", uri, org, repo)
}

// NewIn is New for the repository whose hub is dir: hop.gitDomain is
// resolved there (GitDomainIn).
func NewIn(dir, uri, org, repo string) string {
	if org == "" || repo == "" {
		return ""
	}
	domain := ""
	if Host(uri) == "" {
		domain = GitDomainIn(dir)
	}
	return NewWithDomain(uri, org, repo, domain)
}

// NewWithDomain is New with the fallback host given: domain stands in for
// hop.gitDomain when uri has no host. "" when org or repo is empty.
func NewWithDomain(uri, org, repo, domain string) string {
	if org == "" || repo == "" {
		return ""
	}
	host := Host(uri)
	if host == "" {
		host = domain
	}
	return host + "/" + org + "/" + repo
}

// For returns the ID of the repository a hub or hopspace config
// describes, the hub being at hubPath.
func For(hubPath string, repo config.RepoConfig) string {
	return NewIn(hubPath, repo.URI, repo.Org, repo.Repo)
}

// Split returns the parts of a 3-part repo ID. ok is false when id has
// another shape or an empty part.
func Split(id string) (host, org, repo string, ok bool) {
	parts := strings.Split(id, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}
