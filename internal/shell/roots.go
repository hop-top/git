package shell

import (
	"bufio"
	"fmt"
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
//
// Because it only ever grows, this cannot express a worktree going away.
// Any caller acting on a hub whose branch set may have SHRUNK -- remove,
// move, and anything else that retires a path -- must use RebuildRootsCache
// instead.
func MergeRootsCache(fs afero.Fs, hub *hop.Hub, hubPath string) error {
	fresh := HubWorktreeRoots(hub, hubPath)
	if len(fresh) == 0 {
		return nil
	}
	return WriteRootsCache(fs, append(ReadRootsCache(fs), fresh...))
}

// RebuildRootsCache replaces one hub's entries in the cache with the hub's
// current worktree set, leaving every other hub's entries untouched.
//
// This is the write half of the mutation story MergeRootsCache cannot tell.
// A merge answers "which paths did we just learn about", which is the whole
// question on a switch. It is the wrong question after a remove or a move:
// there the interesting fact is which path stopped being a worktree, and a
// cache that only grows keeps announcing it. The shell then hands the
// binary a path that no longer resolves -- harmless, because the binary
// re-verifies, but it means removal never actually takes effect.
//
// Scoping the replacement to the hub is what keeps the cross-repository
// property intact: entries under this hub are authoritative from its
// hop.json, and anything outside it is carried through verbatim because
// this hub knows nothing about it either way. An empty branch set is
// therefore meaningful here rather than a no-op -- it is exactly how the
// last worktree of a hub gets retired.
//
// The one path this does not retire is a worktree the hub had recorded at
// an absolute location OUTSIDE its own directory. Nothing in the cache
// records which hub put an entry there, so such a line is indistinguishable
// from another repository's, and dropping it would resurrect precisely the
// cross-repository blindness the merge design exists to prevent. Leaving it
// costs one stale string compare per prompt and one re-verified miss if the
// user cds there; dropping the wrong one costs a worktree that is never
// detected again. The cheap wrong answer is the safe direction here.
func RebuildRootsCache(fs afero.Fs, hub *hop.Hub, hubPath string) error {
	if hub == nil || hub.Config == nil {
		return nil
	}

	abs, err := filepath.Abs(hubPath)
	if err != nil {
		return err
	}
	abs = filepath.Clean(abs)

	kept := make([]string, 0, len(hub.Config.Branches))
	for _, r := range ReadRootsCache(fs) {
		if r == abs || strings.HasPrefix(r, abs+string(filepath.Separator)) {
			// This hub's own territory: hop.json is about to restate it.
			continue
		}
		kept = append(kept, r)
	}

	return WriteRootsCache(fs, append(kept, HubWorktreeRoots(hub, hubPath)...))
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

// reloadFunc is the shell function that re-slurps the cache into the
// session's array. Named rather than inlined so the wrapper's call site
// reads as one word and so both halves stay in this file.
const reloadFunc = "__git_hop_reload_roots"

// generateRootsReload emits the reload function plus the initial slurp for
// a shell type, or "" for an unrecognised one.
//
// Writing the cache correctly is only half of keeping plain-`cd` detection
// honest. The array the handler tests $PWD against is filled once, when the
// shell starts, so a worktree added, removed, or renamed later in that
// session never reaches the shell that is running -- the file on disk is
// right and the array in memory is a snapshot of whenever the terminal was
// opened. On a fresh install that snapshot is empty, which is why the
// feature can look like it does nothing at all.
//
// Where the reload goes is the whole question, and the prompt is the one
// place it cannot go. The handler's budget is stated plainly next to it: no
// fork, no stat, no subshell on the miss path, because the miss path is
// nearly every prompt. A generation or mtime check does not escape that --
// it is still a stat per prompt, and the file it stats changes a handful of
// times a day against a handler that runs thousands. Buying freshness with
// per-prompt I/O trades the exact cost the cache was built to avoid.
//
// So the reload hangs off the wrapper instead. Every mutation the user can
// perform arrives as a `git hop` they typed, and the wrapper is the shell
// function that ran it -- in the same shell whose array is stale, at a
// moment where a file read is already lost in the noise of the process that
// just forked, ran git, and touched the filesystem. The prompt path is left
// untouched, and the array is current again before the next prompt draws.
//
// What this does not cover is a mutation made in a DIFFERENT terminal:
// that shell's wrapper reloads, this one's does not, and this one stays
// stale until its next `git hop`. That is the deliberate edge. Closing it
// needs either per-prompt polling (the cost we refused) or a signal path
// no portable shell offers, and the failure it leaves behind is the mild
// direction: a missed detection, self-healing on the next hop, never a
// wrong one -- the binary re-verifies every hit against hop.json anyway.
func generateRootsReload(shellType string) string {
	path := RootsCachePath()

	switch shellType {
	case "bash", "zsh":
		return fmt.Sprintf(`%s() {
    %s=()
    if [[ -r "%s" ]]; then
        while IFS= read -r __git_hop_line; do
            [[ -n "$__git_hop_line" ]] && %s+=("$__git_hop_line")
        done < "%s"
        unset __git_hop_line
    fi
}
`, reloadFunc, rootsVar, path, rootsVar, path)

	case "fish":
		return fmt.Sprintf(`function %s
    set -g %s
    if test -r "%s"
        while read -l __git_hop_line
            if test -n "$__git_hop_line"
                set -g -a %s $__git_hop_line
            end
        end < "%s"
    end
end
`, reloadFunc, rootsVar, path, rootsVar, path)

	default:
		return ""
	}
}

// rootsReloadFor is the generator the wrapper block embeds, trimmed the
// same way the chdir handler is so the emitted block stays tidy.
func rootsReloadFor(shellType string) string {
	return strings.TrimRight(generateRootsReload(shellType), "\n")
}
