package cmd

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"hop.top/git/internal/config"
	"hop.top/git/internal/output"
)

// mise trust modes for hop.add.miseTrust.
const (
	// miseTrustAuto: trust the new worktree's mise config when the
	// source worktree's is trusted and the two match file for file.
	miseTrustAuto = "auto"
	// miseTrustAlways: trust the new worktree's mise config whenever
	// it has one.
	miseTrustAlways = "always"
	// miseTrustNever: leave mise trust alone.
	miseTrustNever = "never"
)

// addMiseTrustFlag / addNoMiseTrustFlag hold the two halves of the
// --[no-]mise-trust pair; each is only consulted when explicitly set.
var (
	addMiseTrustFlag   bool
	addNoMiseTrustFlag bool
)

// miseCLI is the part of mise add drives. trust --show prints one
// "<path>: trusted|untrusted" line per config directory (per file in
// paranoid mode) from dir and its parents.
type miseCLI interface {
	// Available reports whether mise can be run.
	Available() bool
	// TrustShow runs 'mise -C dir trust --show', in paranoid mode when
	// paranoid is set, and returns its stdout.
	TrustShow(dir string, paranoid bool) (string, error)
	// Trust runs 'mise trust dir'.
	Trust(dir string) error
}

// execMise runs the mise found on PATH.
type execMise struct{}

func (execMise) Available() bool {
	_, err := exec.LookPath("mise")
	return err == nil
}

func (execMise) TrustShow(dir string, paranoid bool) (string, error) {
	env := os.Environ()
	if paranoid {
		env = append(env, "MISE_PARANOID=1")
	}
	return runMise(env, "-C", dir, "trust", "--show")
}

func (execMise) Trust(dir string) error {
	_, err := runMise(os.Environ(), "trust", dir)
	return err
}

// runMise runs mise with args and returns its stdout. Its stderr (update
// notices, warnings) is kept for the error only.
func runMise(env []string, args ...string) (string, error) {
	c := exec.Command("mise", args...)
	c.Env = env
	var out, errOut bytes.Buffer
	c.Stdout, c.Stderr = &out, &errOut
	if err := c.Run(); err != nil {
		if msg := strings.TrimSpace(errOut.String()); msg != "" {
			return "", errors.New(msg)
		}
		return "", err
	}
	return out.String(), nil
}

// miseTrustMode decides the mode: --[no-]mise-trust (override) wins,
// then hop.add.miseTrust. --mise-trust is always, --no-mise-trust never.
// An unknown configured value warns and counts as auto.
func miseTrustMode(gc *config.GitConfig, override *bool) string {
	if override != nil {
		if *override {
			return miseTrustAlways
		}
		return miseTrustNever
	}
	v := strings.ToLower(strings.TrimSpace(gc.GetStringOrDefault(config.KeyAddMiseTrust)))
	switch v {
	case miseTrustAuto, miseTrustAlways, miseTrustNever:
		return v
	}
	output.Warn("%s: unknown value %q; using %s", config.KeyAddMiseTrust, v, miseTrustAuto)
	return miseTrustAuto
}

// miseTrustEntries parses trust --show output into path -> trusted for
// the entries at or under root (resolved through symlinks, as mise
// prints them), keyed by their path relative to root.
func miseTrustEntries(out, root string) map[string]bool {
	entries := map[string]bool{}
	rootReal := realPath(root)
	for _, line := range strings.Split(out, "\n") {
		i := strings.LastIndex(line, ": ")
		if i < 0 {
			continue
		}
		path, status := strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+2:])
		if status != "trusted" && status != "untrusted" {
			continue
		}
		rel, err := filepath.Rel(rootReal, realPath(path))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		entries[rel] = status == "trusted"
	}
	return entries
}

// realPath is path with symlinks resolved, or path when that fails.
func realPath(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}
	return filepath.Clean(path)
}

// miseConfigFiles lists the mise config files mise reads in dir itself
// (not its parents), relative to dir, through mise's own paranoid-mode
// listing, which names each file without parsing it.
func miseConfigFiles(m miseCLI, dir string) ([]string, error) {
	out, err := m.TrustShow(dir, true)
	if err != nil {
		return nil, err
	}
	var files []string
	for rel := range miseTrustEntries(out, dir) {
		if rel != "." {
			files = append(files, rel)
		}
	}
	sort.Strings(files)
	return files, nil
}

// miseTrusted reports whether mise trusts the config at or under dir:
// at least one entry, all of them trusted.
func miseTrusted(m miseCLI, dir string) (bool, error) {
	out, err := m.TrustShow(dir, false)
	if err != nil {
		return false, err
	}
	entries := miseTrustEntries(out, dir)
	if len(entries) == 0 {
		return false, nil
	}
	for _, trusted := range entries {
		if !trusted {
			return false, nil
		}
	}
	return true, nil
}

// sameMiseConfig reports whether dst has exactly src's mise config
// files, byte for byte; when not, the first difference in words.
func sameMiseConfig(srcFiles, dstFiles []string, src, dst string) (bool, string) {
	if strings.Join(srcFiles, "\n") != strings.Join(dstFiles, "\n") {
		return false, "its mise config files (" + listOrNone(dstFiles) + ") are not the source's (" + listOrNone(srcFiles) + ")"
	}
	for _, rel := range srcFiles {
		a, errA := os.ReadFile(filepath.Join(src, rel))
		b, errB := os.ReadFile(filepath.Join(dst, rel))
		if errA != nil || errB != nil || !bytes.Equal(a, b) {
			return false, rel + " differs from the source's"
		}
	}
	return true, ""
}

func listOrNone(files []string) string {
	if len(files) == 0 {
		return "none"
	}
	return strings.Join(files, ", ")
}

// trustWorktreeMise trusts the mise config of the new worktree dst as
// mode says, src being the worktree it forked from ("" when there is
// none). It runs after the ignored files are copied (mise.local.toml is
// usually one) and before the environment set-up and post-worktree-add,
// which may need mise. Never fatal: a skip says why on a hint line.
func trustWorktreeMise(m miseCLI, mode, src, dst string) {
	if mode == miseTrustNever {
		return
	}
	if !m.Available() {
		output.Hint("mise not found on PATH; not trusting the new worktree's mise config (silence with 'git config %s %s')",
			config.KeyAddMiseTrust, miseTrustNever)
		return
	}
	dstFiles, err := miseConfigFiles(m, dst)
	if err != nil {
		output.Warn("could not list the new worktree's mise config: %v", err)
		return
	}
	if len(dstFiles) == 0 {
		output.Hint("no mise config in the new worktree; nothing to trust")
		return
	}
	if trusted, err := miseTrusted(m, dst); err == nil && trusted {
		output.Debug("mise config of %s already trusted", dst)
		return
	}
	if mode == miseTrustAuto {
		if why := miseTrustRefusal(m, src, dst, dstFiles); why != "" {
			output.Hint("not trusting the new worktree's mise config: %s; review it, then run 'mise trust %s'", why, dst)
			return
		}
	}
	if err := m.Trust(dst); err != nil {
		output.Warn("could not trust the new worktree's mise config: %v", err)
		return
	}
	if mode == miseTrustAuto {
		output.Info("Trusted mise config in %s (same as trusted %s)", dst, src)
	} else {
		output.Info("Trusted mise config in %s (%s=%s)", dst, config.KeyAddMiseTrust, mode)
	}
}

// miseTrustRefusal says why auto mode does not trust dst's mise config,
// "" when it does: the source must have trusted config, file for file
// the same as dst's.
func miseTrustRefusal(m miseCLI, src, dst string, dstFiles []string) string {
	if src == "" {
		return "there is no source worktree to compare it with"
	}
	trusted, err := miseTrusted(m, src)
	if err != nil {
		return "cannot read the trust of the source worktree " + src + "'s mise config: " + err.Error()
	}
	if !trusted {
		return "the source worktree " + src + " has no trusted mise config"
	}
	srcFiles, err := miseConfigFiles(m, src)
	if err != nil {
		return "cannot list the source worktree's mise config: " + err.Error()
	}
	if same, why := sameMiseConfig(srcFiles, dstFiles, src, dst); !same {
		return why
	}
	return ""
}

// previewMiseTrust reports what trustWorktreeMise would do. The new
// worktree does not exist yet, so only the source side is checked.
func previewMiseTrust(m miseCLI, mode, src string) {
	switch {
	case mode == miseTrustNever:
	case !m.Available():
		output.Info("[dry-run] Would skip mise trust: mise not found on PATH")
	case mode == miseTrustAlways:
		output.Info("[dry-run] Would trust the new worktree's mise config, if it has one (%s=%s)", config.KeyAddMiseTrust, mode)
	case src == "":
		output.Info("[dry-run] Would not trust the new worktree's mise config: no source worktree to compare it with")
	default:
		if trusted, err := miseTrusted(m, src); err != nil || !trusted {
			output.Info("[dry-run] Would not trust the new worktree's mise config: %s has no trusted mise config", src)
			return
		}
		output.Info("[dry-run] Would trust the new worktree's mise config if it matches the trusted config of %s", src)
	}
}
