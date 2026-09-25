package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

// porcelainWorktree is one record from `git worktree list --porcelain`.
// Branch is the short name (without refs/heads/ prefix) when present;
// Bare marks the bare-repo entry that heads the list; Detached marks a
// detached-HEAD worktree (HEAD present, no branch line).
type porcelainWorktree struct {
	Path     string
	Branch   string
	Bare     bool
	Detached bool
}

// parseWorktreeListPorcelain parses `git worktree list --porcelain`
// output into a slice of porcelainWorktree. Records are separated by
// blank lines; each record's first line is "worktree <path>", followed
// by one of "bare", "detached", or "HEAD <sha>\nbranch refs/heads/<n>".
//
// Unknown lines inside a record are tolerated (forward-compatible with
// future git porcelain extensions). Empty input returns nil.
func parseWorktreeListPorcelain(s string) []porcelainWorktree {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []porcelainWorktree
	var cur porcelainWorktree
	flush := func() {
		if cur.Path != "" {
			out = append(out, cur)
		}
		cur = porcelainWorktree{}
	}
	for _, line := range strings.Split(s, "\n") {
		if line == "" {
			flush()
			continue
		}
		switch {
		case strings.HasPrefix(line, "worktree "):
			// New record. If we already had one in progress, push it.
			flush()
			cur.Path = strings.TrimPrefix(line, "worktree ")
		case line == "bare":
			cur.Bare = true
		case line == "detached":
			cur.Detached = true
		case strings.HasPrefix(line, "branch "):
			ref := strings.TrimPrefix(line, "branch ")
			cur.Branch = strings.TrimPrefix(ref, "refs/heads/")
		}
	}
	flush()
	return out
}

// backfillHubConfigIfMissing writes hop.json at hubPath when it is
// missing, populating it from runtime git state. Returns (true, nil) on
// successful write; (false, nil) when hop.json already exists; (false,
// err) on failure.
//
// Inputs gathered:
//   - origin URL via `git remote get-url origin` (best-effort; falls
//     back to the labspace path convention <parent>/<dir> used by
//     registerAsIs).
//   - default branch via `git symbolic-ref HEAD` (falls back to "main"
//     on error, matching CreateHub's behavior).
//   - branches via WorktreeListPorcelain; the bare entry and detached
//     worktrees are skipped.
//
// envPatterns defaults to the same set CreateHub seeds (dev, staging,
// qa) so a backfilled hub behaves identically to a freshly-initialized
// one.
func backfillHubConfigIfMissing(fs afero.Fs, g git.GitInterface, hubPath string) (bool, error) {
	if exists, _ := afero.Exists(fs, filepath.Join(hubPath, "hop.json")); exists {
		return false, nil
	}

	// Repo identity. Empty/error → local-path fallback.
	uri := ""
	if out, err := g.RunInDir(hubPath, "git", "remote", "get-url", "origin"); err == nil {
		uri = strings.TrimSpace(out)
	}
	var org, repo string
	if uri != "" {
		org, repo = hop.ParseRepoFromURL(uri)
	}
	if org == "" || repo == "" {
		abs, err := filepath.Abs(hubPath)
		if err != nil {
			return false, fmt.Errorf("resolve hub path: %w", err)
		}
		repo = filepath.Base(abs)
		org = filepath.Base(filepath.Dir(abs))
	}

	defaultBranch := backfillDefaultBranch(g, hubPath)

	// Worktrees.
	porcelain, err := g.WorktreeListPorcelain(hubPath)
	if err != nil {
		return false, fmt.Errorf("git worktree list: %w", err)
	}
	entries := parseWorktreeListPorcelain(porcelain)

	branches := make(map[string]config.HubBranch)
	for _, w := range entries {
		if w.Bare || w.Detached || w.Branch == "" {
			continue
		}
		branches[w.Branch] = config.HubBranch{
			Path:           w.Path,
			HopspaceBranch: w.Branch,
		}
	}

	cfg := &config.HubConfig{
		Repo: config.RepoConfig{
			URI:           uri,
			Org:           org,
			Repo:          repo,
			DefaultBranch: defaultBranch,
		},
		Branches: branches,
		Settings: config.HubSettings{
			EnvPatterns: []string{"dev", "staging", "qa"},
		},
	}

	writer := config.NewWriter(fs)
	if err := writer.WriteHubConfig(hubPath, cfg); err != nil {
		return false, fmt.Errorf("write hop.json: %w", err)
	}
	return true, nil
}

// backfillDefaultBranch is the default branch a back-filled hop.json
// records for the hub at hubPath: the branch HEAD names, "main" when
// that cannot be read (matching CreateHub).
func backfillDefaultBranch(g git.GitInterface, hubPath string) string {
	if out, err := g.RunInDir(hubPath, "git", "symbolic-ref", "HEAD"); err == nil {
		if v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "refs/heads/")); v != "" {
			return v
		}
	}
	return "main"
}

// resolveBackfillRoot decides where to write hop.json given the
// directory the user invoked `git hop init` from and its detected
// structure. Returns (path, true) for the structures we can backfill,
// ("", false) otherwise.
//
//   - BareWorktreeRoot, WorktreeRoot → cwd is the hub root.
//   - WorktreeChild → the repository the worktree belongs to, from git's
//     common dir (hop.RepoRootOfWorktree), when that repository is a hub:
//     a bare one, or a regular one a --regular conversion left hop.json
//     in. A plain regular repository is not back-filled: that would
//     register it without converting it (see resolveInitTarget).
//
// Other structures (StandardRepo, NotGit, UnknownStructure) are not our
// case: a standard repo gets the conversion menu instead.
func resolveBackfillRoot(fs afero.Fs, g git.GitInterface, cwd string, s config.StructureType) (string, bool) {
	switch s {
	case config.BareWorktreeRoot, config.WorktreeRoot:
		return cwd, true
	case config.WorktreeChild:
		root, rs, ok := repoOfLinkedWorktree(fs, g, cwd)
		if !ok || !isHubStructure(rs) {
			return "", false
		}
		return root, true
	default:
		return "", false
	}
}
