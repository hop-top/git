package cmd

import (
	"fmt"
	"os"
	"strings"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
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

// fetchMode is how add treats origin before resolving the start-point.
type fetchMode int

const (
	// fetchSkip: no fetch.
	fetchSkip fetchMode = iota
	// fetchAuto: fetched because the start-point is an origin ref; a
	// failure only warns.
	fetchAuto
	// fetchRequired: the user asked for it (--fetch, hop.add.fetch true);
	// a failure is fatal.
	fetchRequired
)

// decideFetch picks the fetchMode: --[no-]fetch (override) wins, then
// hop.add.fetch, and with neither set add fetches only when the
// start-point is an origin ref.
func decideFetch(g git.GitInterface, gc *config.GitConfig, override *bool, hubPath, startPoint, defaultBranch string) fetchMode {
	explicit := func(on bool) fetchMode {
		if on {
			return fetchRequired
		}
		return fetchSkip
	}
	if override != nil {
		return explicit(*override)
	}
	if v, err := gc.GetBool(config.KeyAddFetch); err == nil {
		return explicit(v)
	}
	if startPointIsOriginRef(g, hubPath, startPoint, defaultBranch) {
		return fetchAuto
	}
	return fetchSkip
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
// hop.remote.timeout. A requested fetch that fails is fatal. An automatic
// one only warns: the start-point still resolves against the refs already
// present, so add carries on and says they may be stale.
func fetchOrigin(g git.GitInterface, hubPath string, mode fetchMode) {
	err := g.FetchRemote(hubPath, "origin")
	if err == nil {
		return
	}
	if mode == fetchRequired {
		output.Fatal("could not fetch origin: %v\n"+
			"hint: fix the origin remote, or pass --no-fetch to start from the local refs", err)
	}
	fmt.Fprintf(os.Stderr, "warning: could not fetch origin: %v\n"+
		"hint: starting from the local refs, which may be stale\n", err)
}
