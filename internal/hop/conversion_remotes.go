package hop

import (
	"fmt"
	"path/filepath"
	"strings"
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
// replace, and drops every other remote and all upstream settings. This
// swaps that origin for srcRepo's own remote and branch config, then
// copies srcRepo's remote-tracking refs so upstreams resolve without a
// fetch.
func (c *Converter) carryOverRemotes(srcRepo, bareRepo string) error {
	entries, err := c.localConfigEntries(srcRepo, remoteConfigSections)
	if err != nil {
		return fmt.Errorf("failed to read remotes of %s: %w", srcRepo, err)
	}
	if !hasRemote(entries) {
		return nil
	}

	if _, err := c.git.Run("git", "-C", bareRepo, "config", "--remove-section", "remote.origin"); err != nil {
		return fmt.Errorf("failed to drop the clone's origin: %w", err)
	}
	for _, e := range entries {
		if _, err := c.git.Run("git", "-C", bareRepo, "config", "--add", e.key, e.value); err != nil {
			return fmt.Errorf("failed to restore %s: %w", e.key, err)
		}
	}

	return c.copyRemoteTrackingRefs(srcRepo, bareRepo)
}

type configEntry struct{ key, value string }

// localConfigEntries lists repoPath's local config entries under the
// given section prefixes, in file order, multi-valued keys included.
func (c *Converter) localConfigEntries(repoPath string, prefixes []string) ([]configEntry, error) {
	out, err := c.git.Run("git", "-C", repoPath, "config", "--local", "--null", "--list")
	if err != nil {
		return nil, err
	}
	var entries []configEntry
	for _, rec := range strings.Split(out, "\x00") {
		if rec == "" {
			continue
		}
		key, value, _ := strings.Cut(rec, "\n")
		for _, p := range prefixes {
			if strings.HasPrefix(key, p) {
				entries = append(entries, configEntry{key: key, value: value})
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
