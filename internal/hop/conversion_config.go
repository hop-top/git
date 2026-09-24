package hop

import (
	"fmt"
	"path/filepath"
	"strings"

	"hop.top/git/internal/git"
)

// A bare conversion builds the hub with `git clone --bare`, and a clone
// copies none of the source's local config. The owner's rule: everything
// in the repository's .git/config carries over into the hub's config,
// except keys that describe the old layout or storage format, or that
// another conversion step owns.
//
// Deliberately carried, although git init or clone writes them too:
//
//   - core.filemode, core.ignorecase, core.precomposeunicode,
//     core.symlinks: filesystem probes. The hub sits in the same folder
//     on the same filesystem, so the clone probes the same answers; a
//     value that differs anyway is a user override and wins.
//   - core.logallrefupdates: init writes true for a working-tree repo.
//     It is not wrong in a bare hub: ref updates made from the hub keep
//     being logged, as they were before.
//   - gc.*, submodule.*, core.sparsecheckout and every other key: user
//     settings that do not depend on where .git lives. submodule.<name>
//     entries are keyed by name, not path.
//   - include.path and includeIf.<cond>.path with an absolute or ~ path,
//     which resolve the same from the hub's config.

// excludedConfigRules is the documented list of keys a bare conversion
// leaves out. The first matching rule gives the reason.
var excludedConfigRules = []struct {
	match  func(key, value string) bool
	reason string
}{
	{keyIs("core.bare"),
		"the hub is a bare repository"},
	{keyIs("core.worktree"),
		"names the old working tree; hub worktrees live under hops/"},
	{keyIs("core.repositoryformatversion"),
		"set by the clone to match its own storage format"},
	// objectformat, refstorage and compatobjectformat declare how the
	// clone's storage is laid out; the clone writes its own. noop,
	// partialclone and preciousobjects are format declarations in the
	// same namespace. worktreeConfig set to true is not excluded:
	// PlanLocalConfig carries it, and carryOverWorktreeConfig writes it.
	{sectionIs("extensions"),
		"repository format declaration; the clone writes its own"},
	{isRelativeInclude,
		"relative include path would resolve against the hub, not the old .git directory"},
}

func keyIs(want string) func(string, string) bool {
	return func(key, _ string) bool { return strings.EqualFold(key, want) }
}

func sectionIs(want string) func(string, string) bool {
	return func(key, _ string) bool {
		section, _, _ := strings.Cut(key, ".")
		return strings.EqualFold(section, want)
	}
}

// isRelativeInclude matches include.path and includeIf.<cond>.path
// entries whose meaning depends on the directory of the config file that
// holds them: a relative path, or a gitdir condition starting with "./".
// That directory is <repo>/.git before a bare conversion and <repo>
// after it.
func isRelativeInclude(key, value string) bool {
	section, rest, _ := strings.Cut(key, ".")
	lastDot := strings.LastIndex(rest, ".")
	name, sub := rest, ""
	if lastDot >= 0 {
		sub, name = rest[:lastDot], rest[lastDot+1:]
	}
	if !strings.EqualFold(name, "path") {
		return false
	}
	switch {
	case strings.EqualFold(section, "include") && sub == "":
	case strings.EqualFold(section, "includeif"):
		if strings.HasPrefix(sub, "gitdir:./") || strings.HasPrefix(sub, "gitdir/i:./") {
			return true
		}
	default:
		return false
	}
	return !filepath.IsAbs(value) && !strings.HasPrefix(value, "~")
}

// LocalConfigPlan is what a bare conversion does with the repository's
// local config: which entries it writes into the hub and which it leaves
// out, both in the file's order.
type LocalConfigPlan struct {
	Carried  []configEntry
	Excluded []ExcludedConfigEntry

	// WorktreeConfig is true when the repository has
	// extensions.worktreeConfig on, so git reads .git/config.worktree.
	// Its entries are sorted by the same rules into PerWorktree, written
	// to the default worktree's own config.worktree, and
	// PerWorktreeExcluded.
	WorktreeConfig      bool
	PerWorktree         []configEntry
	PerWorktreeExcluded []ExcludedConfigEntry

	// SparseToWorktree is true when the repository is a sparse checkout
	// configured in its shared config, without the extension (git before
	// `git sparse-checkout`, or set by hand). Its sparse keys are moved
	// from Carried to PerWorktree: in the hub's shared config they would
	// make every worktree sparse. See conversion_sparse.go.
	SparseToWorktree bool
}

// HubWorktreeConfig reports whether the hub gets extensions.worktreeConfig,
// with core.bare in its own config.worktree and PerWorktree written to
// the default worktree's config.worktree.
func (p *LocalConfigPlan) HubWorktreeConfig() bool {
	return p.WorktreeConfig || p.SparseToWorktree
}

const worktreeConfigKey = "extensions.worktreeconfig"

func isWorktreeConfigKey(key string) bool {
	return strings.EqualFold(key, worktreeConfigKey)
}

// ExcludedConfigEntry is a local config entry a bare conversion leaves
// out of the hub, and why.
type ExcludedConfigEntry struct {
	Key    string
	Value  string
	Reason string
}

// PlanLocalConfig reads repoPath's local config (the file itself, includes
// not followed) and sorts every entry into carried or excluded.
func PlanLocalConfig(g git.GitInterface, repoPath string) (*LocalConfigPlan, error) {
	entries, err := readLocalConfig(g, repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read the local config of %s: %w", repoPath, err)
	}
	plan := &LocalConfigPlan{}
	// git's own reading of the extension, whatever its spelling.
	if on, err := g.Run("git", "-C", repoPath, "config", "--local", "--type=bool", "--get", "extensions.worktreeConfig"); err == nil && on == "true" {
		plan.WorktreeConfig = true
	}
	for _, e := range entries {
		if plan.WorktreeConfig && isWorktreeConfigKey(e.key) {
			plan.Carried = append(plan.Carried, e)
			continue
		}
		if reason := excludedReason(e.key, e.value); reason != "" {
			plan.Excluded = append(plan.Excluded, ExcludedConfigEntry{Key: e.key, Value: e.value, Reason: reason})
			continue
		}
		plan.Carried = append(plan.Carried, e)
	}
	if plan.WorktreeConfig {
		perWorktree, err := readWorktreeConfig(g, repoPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read the per-worktree config of %s: %w", repoPath, err)
		}
		for _, e := range perWorktree {
			if reason := excludedReason(e.key, e.value); reason != "" {
				plan.PerWorktreeExcluded = append(plan.PerWorktreeExcluded, ExcludedConfigEntry{Key: e.key, Value: e.value, Reason: reason})
				continue
			}
			plan.PerWorktree = append(plan.PerWorktree, e)
		}
	} else {
		planSparseToWorktree(g, repoPath, plan)
	}
	return plan, nil
}

func excludedReason(key, value string) string {
	for _, r := range excludedConfigRules {
		if r.match(key, value) {
			return r.reason
		}
	}
	return ""
}

// CarriedKeys lists the carried key names once each, in file order.
func (p *LocalConfigPlan) CarriedKeys() []string { return uniqueKeys(p.Carried) }

// ExcludedKeys lists the excluded entries once per key and reason, in
// file order.
func (p *LocalConfigPlan) ExcludedKeys() []ExcludedConfigEntry { return uniqueExcluded(p.Excluded) }

// PerWorktreeKeys and PerWorktreeExcludedKeys are CarriedKeys and
// ExcludedKeys for .git/config.worktree.
func (p *LocalConfigPlan) PerWorktreeKeys() []string { return uniqueKeys(p.PerWorktree) }

func (p *LocalConfigPlan) PerWorktreeExcludedKeys() []ExcludedConfigEntry {
	return uniqueExcluded(p.PerWorktreeExcluded)
}

func uniqueKeys(entries []configEntry) []string {
	var keys []string
	seen := map[string]bool{}
	for _, e := range entries {
		if !seen[e.key] {
			seen[e.key] = true
			keys = append(keys, e.key)
		}
	}
	return keys
}

func uniqueExcluded(entries []ExcludedConfigEntry) []ExcludedConfigEntry {
	var out []ExcludedConfigEntry
	seen := map[string]bool{}
	for _, e := range entries {
		id := e.Key + "\x00" + e.Reason
		if !seen[id] {
			seen[id] = true
			out = append(out, ExcludedConfigEntry{Key: e.Key, Reason: e.Reason})
		}
	}
	return out
}

// isRemoteConfig reports whether carryOverRemotes owns key.
func isRemoteConfig(key string) bool {
	for _, p := range remoteConfigSections {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

// carryOverLocalConfig writes the plan's carried entries, other than the
// remote and branch config carryOverRemotes already wrote, into bareRepo.
// A key the clone set itself (a filesystem probe) is replaced, not
// duplicated; every other key is appended value by value, so multi-valued
// keys keep their values and order.
func (c *Converter) carryOverLocalConfig(plan *LocalConfigPlan, bareRepo string) error {
	existing, err := readLocalConfig(c.git, bareRepo)
	if err != nil {
		return fmt.Errorf("failed to read the hub's config: %w", err)
	}
	inHub := map[string]bool{}
	for _, e := range existing {
		inHub[e.key] = true
	}

	for _, e := range plan.Carried {
		// carryOverRemotes and carryOverWorktreeConfig own these.
		if isRemoteConfig(e.key) || isWorktreeConfigKey(e.key) {
			continue
		}
		if inHub[e.key] {
			if _, err := c.git.Run("git", "-C", bareRepo, "config", "--unset-all", e.key); err != nil {
				return fmt.Errorf("failed to replace %s: %w", e.key, err)
			}
			inHub[e.key] = false
		}
		if err := addConfigEntry(c.git, []string{"-C", bareRepo, "config"}, e); err != nil {
			return err
		}
	}
	return nil
}

// addConfigEntry appends e with `git <cfgCmd...> --add`, where cfgCmd
// selects the file ("-C <repo> config" or "config --file <path>").
func addConfigEntry(g git.GitInterface, cfgCmd []string, e configEntry) error {
	value := e.value
	if e.implicit {
		// A key with no "=" is boolean true; the command line cannot
		// write one, and "true" reads the same.
		value = "true"
	}
	args := append(append([]string{}, cfgCmd...), "--add", e.key, value)
	if _, err := g.Run("git", args...); err != nil {
		return fmt.Errorf("failed to restore %s: %w", e.key, err)
	}
	return nil
}

// relativeIncludeWarnings names the relative includes a plan leaves out,
// which the user set up and would otherwise lose silently.
func (p *LocalConfigPlan) relativeIncludeWarnings() []string {
	var out []string
	for _, e := range p.Excluded {
		if isRelativeInclude(e.Key, e.Value) {
			out = append(out, fmt.Sprintf(
				"local config %s=%s not carried over: a relative include resolves against the hub now, not .git/; set it again with an absolute path",
				e.Key, e.Value))
		}
	}
	for _, e := range p.PerWorktreeExcluded {
		if isRelativeInclude(e.Key, e.Value) {
			out = append(out, fmt.Sprintf(
				"per-worktree config %s=%s not carried over: a relative include resolves against the worktree's git dir now, not .git/; set it again with an absolute path",
				e.Key, e.Value))
		}
	}
	return out
}
