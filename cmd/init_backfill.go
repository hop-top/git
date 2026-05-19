package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
	"github.com/spf13/afero"
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

	// Default branch. Empty/error → "main".
	defaultBranch := "main"
	if out, err := g.RunInDir(hubPath, "git", "symbolic-ref", "HEAD"); err == nil {
		v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "refs/heads/"))
		if v != "" {
			defaultBranch = v
		}
	}

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

// resolveBackfillRoot decides where to write hop.json given the
// directory the user invoked `git hop init` from and its detected
// structure. Returns (path, true) for the structures we can backfill,
// ("", false) otherwise.
//
//   - BareWorktreeRoot, WorktreeRoot → cwd is the hub root.
//   - WorktreeChild → derive the hub from the worktree's gitdir pointer.
//     Each child worktree has a .git FILE containing "gitdir: <abs>/
//     worktrees/<name>"; the bare-repo / hub root is the parent of the
//     "worktrees" dir. We can't use hop.FindProjectRoot here because it
//     walks up via DetectRepoStructure, which returns NotGit for the
//     intermediate hops/ directory and aborts the walk.
//
// Other structures (StandardRepo, NotGit, UnknownStructure) are not our
// case: a standard repo gets the conversion menu instead.
func resolveBackfillRoot(fs afero.Fs, cwd string, s config.StructureType) (string, bool) {
	switch s {
	case config.BareWorktreeRoot, config.WorktreeRoot:
		return cwd, true
	case config.WorktreeChild:
		return hubFromWorktreeChild(fs, cwd)
	default:
		return "", false
	}
}

// hubFromWorktreeChild reads <cwd>/.git's "gitdir: ..." line and
// returns the hub root. The gitdir takes one of two shapes depending
// on whether the hub is bare:
//
//   - bare hub (git hop's own layout): "<hub>/worktrees/<name>" → hub
//     is the parent of "worktrees".
//   - regular hub (vanilla `git worktree add` from a non-bare repo):
//     "<hub>/.git/worktrees/<name>" → hub is the grandparent of
//     "worktrees" (i.e. the parent of the ".git" segment).
//
// Returns ("", false) when the .git file is unreadable, the gitdir
// line is absent, or the path doesn't end in ".../worktrees/<name>"
// — the caller falls back to "not our case" rather than guessing.
func hubFromWorktreeChild(fs afero.Fs, cwd string) (string, bool) {
	data, err := afero.ReadFile(fs, filepath.Join(cwd, ".git"))
	if err != nil {
		return "", false
	}
	var gitdir string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "gitdir:") {
			gitdir = strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
			break
		}
	}
	if gitdir == "" {
		return "", false
	}
	// Expect ".../worktrees/<name>". Strip "<name>" → parent should be
	// "worktrees"; otherwise the pointer doesn't refer to a worktree.
	parent := filepath.Dir(gitdir)
	if filepath.Base(parent) != "worktrees" {
		return "", false
	}
	hub := filepath.Dir(parent)
	// Regular-hub shape: hub ends in "/.git". Strip that segment so we
	// return the actual hub root (where hop.json should live), not the
	// ".git" directory.
	if filepath.Base(hub) == ".git" {
		hub = filepath.Dir(hub)
	}
	return hub, true
}
