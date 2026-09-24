package cmd

import (
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
	// fetchNoOrigin: the user asked for it, but the hub has no origin
	// remote, so there is nothing to fetch from. Skipped with a hint: the
	// fatal rule is for an origin that fails, not one that is absent.
	fetchNoOrigin
)

// fetches reports whether m runs 'git fetch origin'.
func (m fetchMode) fetches() bool {
	return m == fetchAuto || m == fetchRequired
}

// decideFetch picks the fetchMode: --[no-]fetch (override) wins, then
// hop.add.fetch, and with neither set add fetches only when the
// start-point is an origin ref.
func decideFetch(g git.GitInterface, gc *config.GitConfig, override *bool, hubPath, startPoint, defaultBranch string) fetchMode {
	explicit := func(on bool) fetchMode {
		switch {
		case !on:
			return fetchSkip
		case !hasOrigin(g, hubPath):
			return fetchNoOrigin
		}
		return fetchRequired
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
	if !hasOrigin(g, hubPath) {
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

// hasOrigin reports whether the hub has an origin remote with a URL.
func hasOrigin(g git.GitInterface, hubPath string) bool {
	url, err := g.GetConfig(hubPath, "remote.origin.url")
	return err == nil && strings.TrimSpace(url) != ""
}

// hintNoOrigin says why a requested fetch did not happen. It runs before
// the dry-run split so the preview and the real run agree.
func hintNoOrigin(mode fetchMode) {
	if mode == fetchNoOrigin {
		output.Hint("no origin remote; skipping the requested fetch")
	}
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
	output.Warn("could not fetch origin: %v", err)
	output.Hint("starting from the local refs, which may be stale")
}
