package hop

import (
	"fmt"
	"path/filepath"
	"strings"

	"hop.top/git/internal/git"
)

// repoIdentity is the uri/org/repo a conversion writes into hop.json and
// keys the backup directory by. It is resolved once, from the repository
// as it was before conversion, so no later step can re-derive it from a
// remote the conversion itself rewrote.
type repoIdentity struct {
	URI  string
	Org  string
	Repo string
	// FromPath is true when there is no origin and Org/Repo are the
	// parent and base names of the repository folder.
	FromPath bool
}

// resolveRepoIdentity derives the identity from origin's URL with the
// parser clone uses. Folder names stand in only when there is no origin.
// The URL is origin's configured one, as clone records the URL it was
// given: `git remote get-url` would apply url.<base>.insteadOf rewrites.
func (c *Converter) resolveRepoIdentity(repoPath string) (repoIdentity, error) {
	if uri, err := c.git.Run("git", "-C", repoPath, "config", "--get", "remote.origin.url"); err == nil && uri != "" {
		org, repo := parseRepoFromURL(uri)
		if org == "" || repo == "" {
			return repoIdentity{}, fmt.Errorf("could not parse org/repo from remote URL %q", uri)
		}
		return repoIdentity{URI: uri, Org: org, Repo: repo}, nil
	}

	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return repoIdentity{}, fmt.Errorf("failed to get absolute path: %w", err)
	}
	return repoIdentity{
		Org:      filepath.Base(filepath.Dir(absPath)),
		Repo:     filepath.Base(absPath),
		FromPath: true,
	}, nil
}

// remoteConfigSections are the local config sections that say where a
// repository fetches from and pushes to: every remote (URLs, refspecs,
// push URLs, ...) and every branch's upstream.
var remoteConfigSections = []string{"remote.", "branch."}

// carryOverRemotes makes the bare clone at bareRepo fetch from and push
// to exactly what srcRepo did. `git clone --bare <srcRepo>` points the
// clone's origin at srcRepo itself, a folder the conversion is about to
// replace (and, after the swap, the hub itself), and drops every other
// remote and all upstream settings. This drops that origin, restores
// srcRepo's own remote and branch config, then copies srcRepo's
// remote-tracking refs so upstreams resolve without a fetch. A source
// with no remotes leaves the hub with none.
func (c *Converter) carryOverRemotes(srcRepo, bareRepo string) error {
	entries, err := c.localConfigEntries(srcRepo, remoteConfigSections)
	if err != nil {
		return fmt.Errorf("failed to read remotes of %s: %w", srcRepo, err)
	}

	if _, err := c.git.Run("git", "-C", bareRepo, "config", "--remove-section", "remote.origin"); err != nil {
		return fmt.Errorf("failed to drop the clone's origin: %w", err)
	}
	for _, e := range entries {
		if err := addConfigEntry(c.git, []string{"-C", bareRepo, "config"}, e); err != nil {
			return err
		}
	}

	if !hasRemote(entries) {
		return nil
	}
	return c.copyRemoteTrackingRefs(srcRepo, bareRepo)
}

// configEntry is one line of a config file as `git config --list` shows
// it: section and name lowercased, subsection as written. implicit marks
// a key written without "=", which git reads as boolean true.
type configEntry struct {
	key, value string
	implicit   bool
}

// readLocalConfig lists every entry of repoPath's own config file, in
// file order, multi-valued keys included. Include directives are listed
// as entries, not followed.
func readLocalConfig(g git.GitInterface, repoPath string) ([]configEntry, error) {
	out, err := g.Run("git", "-C", repoPath, "config", "--local", "--no-includes", "--null", "--list")
	if err != nil {
		return nil, err
	}
	return parseConfigList(out), nil
}

// localConfigEntries lists repoPath's local config entries under the
// given section prefixes, in file order, multi-valued keys included.
func (c *Converter) localConfigEntries(repoPath string, prefixes []string) ([]configEntry, error) {
	all, err := readLocalConfig(c.git, repoPath)
	if err != nil {
		return nil, err
	}
	var entries []configEntry
	for _, e := range all {
		for _, p := range prefixes {
			if strings.HasPrefix(e.key, p) {
				entries = append(entries, e)
				break
			}
		}
	}
	return entries, nil
}

func hasRemote(entries []configEntry) bool {
	for _, e := range entries {
		if strings.HasPrefix(e.key, "remote.") && strings.HasSuffix(e.key, ".url") {
			return true
		}
	}
	return false
}

// copyRemoteTrackingRefs copies refs/remotes/* from srcRepo into bareRepo,
// keeping symbolic ones (origin/HEAD) symbolic.
func (c *Converter) copyRemoteTrackingRefs(srcRepo, bareRepo string) error {
	if _, err := c.git.Run("git", "-C", bareRepo, "fetch", "--no-tags", "--quiet",
		srcRepo, "+refs/remotes/*:refs/remotes/*"); err != nil {
		return fmt.Errorf("failed to copy remote-tracking refs: %w", err)
	}

	out, err := c.git.Run("git", "-C", srcRepo, "for-each-ref",
		"--format=%(refname) %(symref)", "refs/remotes")
	if err != nil {
		return fmt.Errorf("failed to list remote-tracking refs: %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		name, target, _ := strings.Cut(strings.TrimSpace(line), " ")
		if name == "" || target == "" {
			continue
		}
		if _, err := c.git.Run("git", "-C", bareRepo, "symbolic-ref", name, target); err != nil {
			return fmt.Errorf("failed to restore %s: %w", name, err)
		}
	}
	return nil
}
