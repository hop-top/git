package services

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
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

// HubComposeProjectName is the compose project a worktree of branch in
// the hub at hubPath runs as: ComposeProjectName with the hub key after
// the repository, so hubs of one repository never share containers.
func HubComposeProjectName(org, repo, hubPath, branch string) string {
	return ComposeProjectName(org, repo+"-"+HubKey(hubPath), branch)
}

// EnvProjectName returns the compose project the worktree at
// worktreePath on branch, in the hub at hubPath, runs as: the one its
// ports.json entry records, else <org>-<repo>-<branch>, as earlier
// releases ran every environment.
func EnvProjectName(fs afero.Fs, hubPath, worktreePath, branch, org, repo string) string {
	hopspace := hubPath
	if hub, err := hop.LoadHub(fs, hubPath); err == nil {
		hopspace = hop.ResolveHopspacePath(hubPath, hub.Config.Repo)
	}
	if hopspace != "" {
		if cfg, err := config.NewLoader(fs).LoadPortsConfig(hopspace); err == nil {
			if e, ok := LookupEnvEntry(cfg, hopspace, hubPath, worktreePath, branch); ok && e.Project != "" {
				return e.Project
			}
		}
	}
	return ComposeProjectName(org, repo, branch)
}
