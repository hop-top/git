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
	// clone's storage is laid out; the clone writes its own. worktreeConfig
	// switches on per-worktree config files (.git/config.worktree) that a
	// conversion does not carry, and noop, partialclone and preciousobjects
	// are format declarations in the same namespace.
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
	for _, e := range entries {
		if reason := excludedReason(e.key, e.value); reason != "" {
			plan.Excluded = append(plan.Excluded, ExcludedConfigEntry{Key: e.key, Value: e.value, Reason: reason})
			continue
		}
		plan.Carried = append(plan.Carried, e)
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
func (p *LocalConfigPlan) CarriedKeys() []string {
	var keys []string
	seen := map[string]bool{}
	for _, e := range p.Carried {
		if !seen[e.key] {
			seen[e.key] = true
			keys = append(keys, e.key)
		}
	}
	return keys
}

// ExcludedKeys lists the excluded entries once per key and reason, in
// file order.
func (p *LocalConfigPlan) ExcludedKeys() []ExcludedConfigEntry {
	var out []ExcludedConfigEntry
	seen := map[string]bool{}
	for _, e := range p.Excluded {
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
		if isRemoteConfig(e.key) {
			continue
		}
		if inHub[e.key] {
			if _, err := c.git.Run("git", "-C", bareRepo, "config", "--unset-all", e.key); err != nil {
				return fmt.Errorf("failed to replace %s: %w", e.key, err)
			}
			inHub[e.key] = false
		}
		if err := addConfigEntry(c.git, bareRepo, e); err != nil {
			return err
		}
	}
	return nil
}

func addConfigEntry(g git.GitInterface, repo string, e configEntry) error {
	value := e.value
	if e.implicit {
		// A key with no "=" is boolean true; the command line cannot
		// write one, and "true" reads the same.
		value = "true"
	}
	if _, err := g.Run("git", "-C", repo, "config", "--add", e.key, value); err != nil {
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
	return out
}
