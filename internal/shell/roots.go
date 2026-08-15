package shell

import (
	"bufio"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/afero"
	"hop.top/git/internal/hop"
)

// RootsCacheName is the file, inside git-hop's cache dir, holding the
// absolute worktree paths the shell integration prefix-tests $PWD against.
const RootsCacheName = "worktree-roots"

// RootsCachePath is the absolute path of the worktree-roots cache.
func RootsCachePath() string {
	return filepath.Join(hop.GetGitHopCacheHome(), RootsCacheName)
}

// WriteRootsCache persists roots as the shell integration's lookup table:
// one absolute path per line, sorted, deduplicated.
//
// The format is deliberately the dumbest thing that works. The chdir
// handler runs on every shell prompt, so whatever it reads has to be
// consumable by shell builtins alone -- a JSON document (hop.json, the
// global registry) would force a `jq`/`git hop` fork per prompt, which is
// exactly the cost this whole design exists to avoid. Newline-delimited
// plain paths can be slurped into an array once per shell session and then
// prefix-tested with pure parameter expansion.
//
// Paths containing a newline are dropped rather than written: they would
// desync every subsequent line, and a mis-parsed cache silently mis-detects
// worktrees instead of failing loudly.
func WriteRootsCache(fs afero.Fs, roots []string) error {
	dir := filepath.Dir(RootsCachePath())
	if err := fs.MkdirAll(dir, 0755); err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(roots))
	clean := make([]string, 0, len(roots))
	for _, r := range roots {
		if r == "" || strings.ContainsAny(r, "\n\r") {
			continue
		}
		abs, err := filepath.Abs(r)
		if err != nil {
			continue
		}
		abs = filepath.Clean(abs)
		if _, dup := seen[abs]; dup {
			continue
		}
		seen[abs] = struct{}{}
		clean = append(clean, abs)
	}
	sort.Strings(clean)

	var b strings.Builder
	for _, r := range clean {
		b.WriteString(r)
		b.WriteByte('\n')
	}

	return afero.WriteFile(fs, RootsCachePath(), []byte(b.String()), 0644)
}

// ReadRootsCache returns the cached worktree paths, or nil when the cache
// is absent. A missing cache is the normal pre-first-hop state, not an
// error: the handler simply detects nothing until the binary next runs.
func ReadRootsCache(fs afero.Fs) []string {
	f, err := fs.Open(RootsCachePath())
	if err != nil {
		return nil
	}
	defer f.Close()

	var roots []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			roots = append(roots, line)
		}
	}
	return roots
}

// HubWorktreeRoots lists a hub's registered worktrees as absolute paths.
//
// The hub's hop.json is the authority on which worktrees exist -- the
// global hops.json registry is written only by some code paths and drifts,
// so trusting it would leave real worktrees undetected. Relative recorded
// paths are anchored on the hub, matching how the switch path resolves
// them, so a worktree recorded as "hops/main" and one recorded absolutely
// both land on the same string the shell will see in $PWD.
func HubWorktreeRoots(hub *hop.Hub, hubPath string) []string {
	if hub == nil || hub.Config == nil {
		return nil
	}

	roots := make([]string, 0, len(hub.Config.Branches))
	for _, b := range hub.Config.Branches {
		p := b.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(hubPath, p)
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		roots = append(roots, filepath.Clean(abs))
	}
	return roots
}

// MergeRootsCache folds a hub's worktrees into the existing cache and
// writes it back.
//
// Merging rather than replacing is what makes the cache work across
// repositories: a user hops in repo A, then cds by hand into a worktree of
// repo B they have not hopped into this session. A replace-on-write cache
// would have dropped B's paths on A's hop and gone blind to it. Entries
// only ever accumulate here; pruning stale ones is the job of the code that
// removes worktrees, not of the prompt-hot path.
func MergeRootsCache(fs afero.Fs, hub *hop.Hub, hubPath string) error {
	fresh := HubWorktreeRoots(hub, hubPath)
	if len(fresh) == 0 {
		return nil
	}
	return WriteRootsCache(fs, append(ReadRootsCache(fs), fresh...))
}

// LookupRoot returns the cached worktree path containing dir, or "" when
// dir is not inside any registered worktree.
//
// This is the Go mirror of the test the shell handler performs inline, and
// exists so the binary can re-verify a shell-side hit before firing a hook:
// the cache can be stale (worktree removed since it was written), and the
// shell's job is only to be cheap, not to be authoritative.
//
// The longest match wins so nested worktrees resolve to the innermost one.
func LookupRoot(roots []string, dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	abs = filepath.Clean(abs)

	best := ""
	for _, r := range roots {
		if abs == r || strings.HasPrefix(abs, r+string(filepath.Separator)) {
			if len(r) > len(best) {
				best = r
			}
		}
	}
	return best
}
