package hooks

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/spf13/afero"

	"hop.top/git/internal/hop"
	"hop.top/git/internal/output"
)

// Install modes for mirroring committed .git-hop/hooks/ into hopspace.
const (
	ModeSymlink = "symlink"
	ModeCopy    = "copy"
	ModePrompt  = "prompt"
	ModeNone    = "none"
)

// MirrorOpts controls how MirrorCommittedHooks behaves.
type MirrorOpts struct {
	// WorktreePath is the just-cloned worktree containing .git-hop/hooks/.
	WorktreePath string
	// RepoID is the 3-part identifier "host/org/repo" (e.g. "github.com/foo/bar").
	RepoID string
	// Mode is one of ModeSymlink, ModeCopy, ModePrompt, ModeNone.
	Mode string
	// Overwrite, when true, replaces an existing hopspace hook with different
	// content in symlink/copy modes. Has no effect in prompt mode (which
	// always asks).
	Overwrite bool
	// Stdin is read for prompt mode. If nil, ModePrompt degrades to ModeNone
	// with an info line.
	Stdin io.Reader
	// Stdout carries the interactive prompt. Defaults to os.Stdout when
	// nil. Warnings and notes go through the output package, so -q and
	// JSON mode apply to them.
	Stdout io.Writer
	// Interactive forces interactive mode regardless of Stdin. Useful for
	// tests; production callers should leave this false and rely on TTY
	// detection by the caller.
	Interactive bool
	// DryRun makes every decision a real run makes but writes nothing and
	// asks nothing: a hook a real run would install is reported with
	// status "would-install", one it would ask about with "would-prompt".
	DryRun bool
}

// HookOutcome describes what happened to one hook during MirrorCommittedHooks.
type HookOutcome struct {
	Name   string // hook filename
	Status string // "installed", "skipped", "already-present", "warned"; on a dry run "would-install", "would-prompt"
	Reason string // optional human-readable reason
	Target string // installed location (when applicable)
}

// Result aggregates the outcomes for a MirrorCommittedHooks invocation.
type Result struct {
	Installed      int
	Skipped        int
	AlreadyPresent int
	Warned         int
	Hooks          []HookOutcome
}

// validHookSet is the set form of ValidHookNames for O(1) lookup.
var validHookSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(ValidHookNames))
	for _, n := range ValidHookNames {
		m[n] = struct{}{}
	}
	return m
}()

// MirrorCommittedHooks scans <WorktreePath>/.git-hop/hooks/ for committed
// hook scripts matching ValidHookNames and mirrors them into the user's
// hopspace at <XDG_DATA_HOME>/git-hop/<host>/<org>/<repo>/hooks/<name>
// according to opts.Mode.
//
// Returns a Result with per-hook outcomes; only system-level errors are
// returned via err (per-hook problems are surfaced as warnings/skips).
func MirrorCommittedHooks(fs afero.Fs, opts MirrorOpts) (Result, error) {
	res := Result{}

	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}

	mode := opts.Mode
	if mode == "" {
		mode = ModePrompt
	}

	if !validMode(mode) {
		return res, fmt.Errorf("invalid hooks install mode: %q (want symlink|copy|prompt|none)", mode)
	}

	srcDir := filepath.Join(opts.WorktreePath, ".git-hop", "hooks")
	exists, err := afero.DirExists(fs, srcDir)
	if err != nil {
		return res, fmt.Errorf("stat %s: %w", srcDir, err)
	}
	if !exists {
		// Most repos don't commit hooks; silent no-op.
		return res, nil
	}

	// Non-interactive prompt mode degrades to none.
	if mode == ModePrompt && opts.Stdin == nil && !opts.Interactive {
		output.Hint("Committed hooks were not mirrored: stdin is non-interactive, so --hooks=prompt cannot ask.\n"+
			"Run %s to mirror them.", remirrorCommand(opts.WorktreePath, ModeSymlink))
		return res, nil
	}

	if mode == ModeNone {
		output.Hint("Committed hooks were not mirrored (hooks mode is none).\n"+
			"Run %s to mirror them.", remirrorCommand(opts.WorktreePath, ModeSymlink))
		return res, nil
	}

	entries, err := afero.ReadDir(fs, srcDir)
	if err != nil {
		return res, fmt.Errorf("read %s: %w", srcDir, err)
	}

	// Sort entries by name for deterministic prompt order.
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	// Resolve hopspace hooks dir.
	parts := strings.Split(opts.RepoID, "/")
	if len(parts) < 3 {
		return res, fmt.Errorf("invalid repoID %q: expected host/org/repo", opts.RepoID)
	}
	hopspaceHooksDir := filepath.Join(hop.GetGitHopDataHome(), parts[0], parts[1], parts[2], "hooks")
	if !opts.DryRun {
		if err := fs.MkdirAll(hopspaceHooksDir, 0755); err != nil {
			return res, fmt.Errorf("create hopspace hooks dir: %w", err)
		}
	}

	// Prompt-mode session state: 'a' (all-yes) or 's' (skip-all).
	var allYes, skipAll bool
	var reader *bufio.Reader
	if mode == ModePrompt {
		reader = bufio.NewReader(opts.Stdin)
	}

	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if _, ok := validHookSet[name]; !ok {
			// Skip non-hook files silently.
			continue
		}
		srcPath := filepath.Join(srcDir, name)
		dstPath := filepath.Join(hopspaceHooksDir, name)

		// Executable check (skip on Windows where mode bits differ).
		info, err := fs.Stat(srcPath)
		if err != nil {
			res.Warned++
			res.Hooks = append(res.Hooks, HookOutcome{Name: name, Status: "warned", Reason: fmt.Sprintf("stat: %v", err)})
			output.Warn("hook %s: %v", name, err)
			continue
		}
		if runtime.GOOS != "windows" && info.Mode()&0111 == 0 {
			res.Warned++
			res.Hooks = append(res.Hooks, HookOutcome{Name: name, Status: "warned", Reason: "not executable"})
			output.Warn("hook %s is not executable; skipping", name)
			output.Hint("Run 'chmod +x .git-hop/hooks/%s', then %s to mirror it.",
				name, remirrorCommand(opts.WorktreePath, mode))
			continue
		}

		// Compare with existing hopspace hook.
		dstExists, _ := afero.Exists(fs, dstPath)
		identical := false
		if dstExists {
			same, err := filesIdentical(fs, srcPath, dstPath)
			if err == nil && same {
				identical = true
			}
		}

		if identical {
			res.AlreadyPresent++
			res.Hooks = append(res.Hooks, HookOutcome{Name: name, Status: "already-present"})
			continue
		}

		// Mode dispatch.
		switch mode {
		case ModeSymlink, ModeCopy:
			if dstExists && !opts.Overwrite {
				res.Warned++
				res.Hooks = append(res.Hooks, HookOutcome{Name: name, Status: "warned", Reason: "exists, no overwrite"})
				output.Warn("hopspace hook %s already exists with different content; pass --hooks-overwrite to replace", name)
				continue
			}
			if opts.DryRun {
				res.Hooks = append(res.Hooks, HookOutcome{Name: name, Status: "would-install", Target: dstPath})
				continue
			}
			if err := installHook(fs, mode, srcPath, dstPath, info); err != nil {
				res.Warned++
				res.Hooks = append(res.Hooks, HookOutcome{Name: name, Status: "warned", Reason: err.Error()})
				output.Warn("failed to install hook %s: %v", name, err)
				continue
			}
			res.Installed++
			res.Hooks = append(res.Hooks, HookOutcome{Name: name, Status: "installed", Target: dstPath})

		case ModePrompt:
			if opts.DryRun {
				res.Hooks = append(res.Hooks, HookOutcome{Name: name, Status: "would-prompt", Target: dstPath})
				continue
			}
			if skipAll {
				res.Skipped++
				res.Hooks = append(res.Hooks, HookOutcome{Name: name, Status: "skipped", Reason: "skip-all"})
				continue
			}
			install := allYes
			if !install {
				answer, err := promptInstall(fs, reader, stdout, name, srcPath, dstPath, dstExists)
				if err != nil {
					return res, fmt.Errorf("prompt: %w", err)
				}
				switch answer {
				case "y":
					install = true
				case "n":
					install = false
				case "a":
					install = true
					allYes = true
				case "s":
					skipAll = true
					install = false
				default:
					install = false
				}
			}
			if !install {
				res.Skipped++
				res.Hooks = append(res.Hooks, HookOutcome{Name: name, Status: "skipped"})
				continue
			}
			// In prompt mode, default to symlink (live tracking).
			if err := installHook(fs, ModeSymlink, srcPath, dstPath, info); err != nil {
				res.Warned++
				res.Hooks = append(res.Hooks, HookOutcome{Name: name, Status: "warned", Reason: err.Error()})
				output.Warn("failed to install hook %s: %v", name, err)
				continue
			}
			res.Installed++
			res.Hooks = append(res.Hooks, HookOutcome{Name: name, Status: "installed", Target: dstPath})
		}
	}

	return res, nil
}

// remirrorCommand names the command that mirrors committed hooks into
// the hopspace after the fact: init re-run in the worktree mirrors again.
func remirrorCommand(worktreePath, mode string) string {
	return fmt.Sprintf("'git hop init --hooks=%s' in %s", mode, worktreePath)
}

func validMode(m string) bool {
	switch m {
	case ModeSymlink, ModeCopy, ModePrompt, ModeNone:
		return true
	}
	return false
}

// installHook installs srcPath at dstPath using the given mode (symlink or
// copy). For copy mode the source mode bits are preserved (chmod 0755).
// If dstPath exists, it is removed first.
func installHook(fs afero.Fs, mode, srcPath, dstPath string, srcInfo os.FileInfo) error {
	// Remove existing destination (file or symlink) before writing.
	if err := removePathIfPresent(fs, dstPath); err != nil {
		return err
	}

	switch mode {
	case ModeSymlink:
		// afero.MemMapFs has no symlinks; tests on it use mode=copy.
		absSrc, err := filepath.Abs(srcPath)
		if err != nil {
			return fmt.Errorf("abs src: %w", err)
		}
		linker, ok := fs.(afero.Linker)
		if !ok {
			return fmt.Errorf("symlink: %w", afero.ErrNoSymlink)
		}
		if err := linker.SymlinkIfPossible(absSrc, dstPath); err != nil {
			return fmt.Errorf("symlink: %w", err)
		}
		return nil

	case ModeCopy:
		data, err := afero.ReadFile(fs, srcPath)
		if err != nil {
			return fmt.Errorf("read src: %w", err)
		}
		if err := afero.WriteFile(fs, dstPath, data, 0755); err != nil {
			return fmt.Errorf("write dst: %w", err)
		}
		// Best-effort chmod to preserve executable bit (no-op on
		// in-memory FS but harmless).
		_ = fs.Chmod(dstPath, 0755)
		return nil

	default:
		return fmt.Errorf("unsupported install mode: %q", mode)
	}
}

func removePathIfPresent(fs afero.Fs, path string) error {
	if err := fs.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove existing %s: %w", path, err)
	}
	return nil
}

// filesIdentical reports whether two files have identical byte content.
func filesIdentical(fs afero.Fs, a, b string) (bool, error) {
	dataA, err := afero.ReadFile(fs, a)
	if err != nil {
		return false, err
	}
	dataB, err := afero.ReadFile(fs, b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(dataA, dataB), nil
}

// promptInstall asks the user whether to install a hook. Returns one of
// "y", "n", "a", "s" (or "d" for diff which loops).
func promptInstall(fs afero.Fs, reader *bufio.Reader, stdout io.Writer, name, srcPath, dstPath string, dstExists bool) (string, error) {
	for {
		if dstExists {
			fmt.Fprintf(stdout, "Hook %s already exists in hopspace with different content.\n", name)
			fmt.Fprintf(stdout, "Install hook %s? [y/N/d/a/s] (y=yes, n=no, d=diff, a=all-yes, s=skip-all): ", name)
		} else {
			previewHook(fs, stdout, name, srcPath)
			fmt.Fprintf(stdout, "Install hook %s? [y/N/a/s] (y=yes, n=no, a=all-yes, s=skip-all): ", name)
		}
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return "", err
		}
		line = strings.ToLower(strings.TrimSpace(line))
		switch line {
		case "y", "yes":
			return "y", nil
		case "", "n", "no":
			return "n", nil
		case "a", "all":
			return "a", nil
		case "s", "skip":
			return "s", nil
		case "d", "diff":
			if dstExists {
				printDiff(fs, stdout, dstPath, srcPath)
				continue
			}
			fmt.Fprintln(stdout, "(no existing hopspace hook to diff against)")
			continue
		default:
			fmt.Fprintln(stdout, "Invalid choice. Please answer y, n, d, a, or s.")
			continue
		}
	}
}

// previewHook prints the hook filename and first 30 lines (or full file).
func previewHook(fs afero.Fs, w io.Writer, name, path string) {
	fmt.Fprintf(w, "--- %s ---\n", name)
	data, err := afero.ReadFile(fs, path)
	if err != nil {
		fmt.Fprintf(w, "(failed to read: %v)\n", err)
		return
	}
	lines := strings.Split(string(data), "\n")
	limit := 30
	if len(lines) < limit {
		limit = len(lines)
	}
	for i := 0; i < limit; i++ {
		fmt.Fprintln(w, lines[i])
	}
	if len(lines) > 30 {
		fmt.Fprintf(w, "... (%d more lines)\n", len(lines)-30)
	}
}

// printDiff emits a simple line-by-line diff between two files.
// Format: "- " for lines only in `existing`, "+ " for lines only in `incoming`.
func printDiff(fs afero.Fs, w io.Writer, existing, incoming string) {
	a, errA := afero.ReadFile(fs, existing)
	b, errB := afero.ReadFile(fs, incoming)
	if errA != nil || errB != nil {
		fmt.Fprintf(w, "(diff unavailable: read err)\n")
		return
	}
	aLines := strings.Split(string(a), "\n")
	bLines := strings.Split(string(b), "\n")
	fmt.Fprintf(w, "--- %s\n+++ %s\n", existing, incoming)
	// Naive line-by-line: shows differences in order. Good enough for the
	// common case (small hooks); not a full Myers diff.
	max := len(aLines)
	if len(bLines) > max {
		max = len(bLines)
	}
	for i := 0; i < max; i++ {
		var aLine, bLine string
		if i < len(aLines) {
			aLine = aLines[i]
		}
		if i < len(bLines) {
			bLine = bLines[i]
		}
		if aLine == bLine {
			continue
		}
		if i < len(aLines) {
			fmt.Fprintf(w, "- %s\n", aLine)
		}
		if i < len(bLines) {
			fmt.Fprintf(w, "+ %s\n", bLine)
		}
	}
}

// ResolveMode picks the install mode following the documented precedence:
//  1. flag (CLI string)
//  2. GIT_HOP_HOOKS env var
//  3. configured (git config hop.hooks.installMode)
//  4. built-in default ("prompt")
func ResolveMode(flag, env, configured string) string {
	if flag != "" {
		return flag
	}
	if env != "" {
		return env
	}
	if configured != "" {
		return configured
	}
	return ModePrompt
}
