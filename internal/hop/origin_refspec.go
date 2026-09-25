package hop

import (
	"strings"

	"hop.top/git/internal/git"
)

// OriginFetchRefspec is the fetch refspec `git clone` writes for origin.
// `git clone --bare` leaves it out, and without it `git fetch origin`
// updates FETCH_HEAD only: refs/remotes/origin/* never moves.
const OriginFetchRefspec = "+refs/heads/*:refs/remotes/origin/*"

// MissingOriginFetchRefspec reports whether the repository at repoPath
// has an origin URL but no origin fetch refspec, the shape a plain
// `git clone --bare` leaves. A repository without origin is not missing
// anything: there is nothing to fetch from.
func MissingOriginFetchRefspec(g git.GitInterface, repoPath string) bool {
	url, err := g.GetConfig(repoPath, "remote.origin.url")
	if err != nil || strings.TrimSpace(url) == "" {
		return false
	}
	// `git config --get` exits non-zero when the key is absent.
	fetch, err := g.GetConfig(repoPath, "remote.origin.fetch")
	return err != nil || strings.TrimSpace(fetch) == ""
}

// RestoreOriginFetchRefspec writes OriginFetchRefspec into the
// repository at repoPath when MissingOriginFetchRefspec says it lacks
// one, and reports whether it wrote. It does not fetch: the next fetch
// of origin populates refs/remotes/origin/*.
func RestoreOriginFetchRefspec(g git.GitInterface, repoPath string) (bool, error) {
	if !MissingOriginFetchRefspec(g, repoPath) {
		return false, nil
	}
	if _, err := g.RunInDir(repoPath, "git", "config", "remote.origin.fetch", OriginFetchRefspec); err != nil {
		return false, err
	}
	return true, nil
}
