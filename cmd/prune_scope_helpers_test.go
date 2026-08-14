package cmd

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/config"
)

// writePruneHubFor is writePruneHub with an explicit org/repo, so tests can
// stand up two hubs belonging to different repositories.
func writePruneHubFor(t *testing.T, fs afero.Fs, hubPath, org, repo string, branches, present []string) {
	t.Helper()
	cfg := &config.HubConfig{
		Repo: config.RepoConfig{
			URI:           "git@github.com:" + org + "/" + repo + ".git",
			Org:           org,
			Repo:          repo,
			DefaultBranch: "main",
		},
		Branches: map[string]config.HubBranch{},
	}
	for _, b := range branches {
		cfg.Branches[b] = config.HubBranch{
			Path:           config.MakeWorktreePath(b),
			HopspaceBranch: b,
		}
	}
	require.NoError(t, fs.MkdirAll(hubPath, 0o755))
	require.NoError(t, config.NewWriter(fs).WriteHubConfig(hubPath, cfg))
	for _, b := range present {
		require.NoError(t, fs.MkdirAll(filepath.Join(hubPath, config.MakeWorktreePath(b)), 0o755))
	}
}

// veryOld returns a timestamp comfortably past any plausible backup
// retention window.
func veryOld() time.Time { return time.Now().Add(-365 * 24 * time.Hour) }

// pruneLines returns the "Pruning ..." lines from captured output — the
// ones that report a mutation and therefore must name their repository.
func pruneLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Pruning ") || strings.HasPrefix(line, "[dry-run] Would prune ") {
			lines = append(lines, line)
		}
	}
	return lines
}
