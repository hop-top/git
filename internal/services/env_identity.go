package services

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"hop.top/git/internal/hop"
	"hop.top/git/internal/state"
)

// Names of the files a compose override directory holds.
const (
	overrideFileName     = "docker-compose.override.yml"
	overrideMetaFileName = ".override-meta.json"
)

// HubKey names a hub where two hubs of one repository must not share a
// name: the hub directory's name, slugged, then 8 hex digits of a hash of
// its resolved path, e.g. "hub-1a2b3c4d". It is compose-safe
// (composeSlugify), and stable for as long as the hub stays where it is.
func HubKey(hubPath string) string {
	resolved := state.ResolvePath(hubPath)
	sum := sha256.Sum256([]byte(resolved))
	hash := hex.EncodeToString(sum[:4])
	if name := composeSlugify(filepath.Base(resolved)); name != "" {
		return name + "-" + hash
	}
	return hash
}

// HubOverrideDir is where the compose override of the hub's worktree of
// branch is cached: <cache>/<org>/<repo>/<hub key>/<branch>.
func HubOverrideDir(org, repo, hubPath, branch string) string {
	return filepath.Join(hop.GetGitHopCacheHome(), org, repo, HubKey(hubPath), branch)
}

// legacyOverrideDir is where earlier releases cached the override of
// branch, one directory for every hub of the repository.
func legacyOverrideDir(org, repo, branch string) string {
	return filepath.Dir(hop.GetComposeOverrideCachePath(org, repo, branch))
}
