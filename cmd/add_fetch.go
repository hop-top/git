package cmd

import (
	"fmt"
	"os"
	"strings"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

// addFetchFlag / addNoFetchFlag hold the two halves of the --[no-]fetch
// pair. Each is only consulted when explicitly set.
var (
	addFetchFlag   bool
	addNoFetchFlag bool
)

// originRefPrefixes are the spellings of a start-point that names a
// remote-tracking branch of origin.
var originRefPrefixes = []string{"refs/remotes/origin/", "remotes/origin/", "origin/"}

// shouldFetchOrigin decides whether add refreshes origin before resolving
// startPoint: --[no-]fetch (override) wins, then hop.add.fetch, and with
// neither set add fetches only when the start-point is an origin ref.
func shouldFetchOrigin(g git.GitInterface, gc *config.GitConfig, override *bool, hubPath, startPoint, defaultBranch string) bool {
	if override != nil {
		return *override
	}
	if v, err := gc.GetBool(config.KeyAddFetch); err == nil {
		return v
	}
	return startPointIsOriginRef(g, hubPath, startPoint, defaultBranch)
}

// startPointIsOriginRef reports whether startPoint resolves through a
// remote-tracking branch of origin: the default branch (which
// WorktreeManager resolves via origin/<default>) or an explicit origin/<x>
// that no local branch of the same name shadows.
func startPointIsOriginRef(g git.GitInterface, hubPath, startPoint, defaultBranch string) bool {
	if url, err := g.GetConfig(hubPath, "remote.origin.url"); err != nil || strings.TrimSpace(url) == "" {
		return false
	}
	switch startPoint {
	case "", hop.StartPointDefaultBranch:
		return defaultBranch != ""
	case hop.StartPointInitial:
		return false
	}
	if refExists(g, hubPath, "refs/heads/"+startPoint) {
		return false
	}
	for _, p := range originRefPrefixes {
		if strings.HasPrefix(startPoint, p) {
			return true
		}
	}
	return false
}

// fetchOrigin refreshes origin's remote-tracking branches, bounded by
// hop.remote.timeout. Failure is not fatal: the start-point still resolves
// against the refs already present, so add warns that they may be stale
// and carries on.
func fetchOrigin(g git.GitInterface, hubPath string) {
	if err := g.FetchRemote(hubPath, "origin"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not fetch origin: %v\n"+
			"hint: starting from the local refs, which may be stale\n", err)
	}
}
