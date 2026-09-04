package services

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
)

// markerFixture builds a source worktree whose .gitignore is the given
// text, plus the ignored entries named in files, and wires a fake git that
// attributes each entry to the .gitignore line holding its pattern.
func markerFixture(t *testing.T, gitignore string, files map[string]string, rules map[string]fakeIgnoreRule) (afero.Fs, string, string, *fakeIgnoredGit) {
	t.Helper()
	files[".gitignore"] = gitignore
	fs, src, dst := newCopyFixture(t, files)

	var status []string
	for p := range rules {
		status = append(status, "!! "+p)
	}
	g := &fakeIgnoredGit{out: strings.Join(status, "\n"), rules: rules}
	return fs, src, dst, g
}

// The marker on the comment line directly above a pattern excludes every
// entry that pattern ignores. Entries decided by unmarked patterns still
// copy — the marker is per rule, not per file.
func TestCopyIgnored_MarkedPatternSkipsItsEntries(t *testing.T) {
	gitignore := strings.Join([]string{
		"# local task state, hub-owned",
		IgnoredCopyMarker,
		".tlc/",
		".env",
		"",
		"#-hop-# build output",
		"dist/",
	}, "\n")
	fs, src, dst, g := markerFixture(t, gitignore, map[string]string{
		".tlc/db.sqlite": "x",
		".env":           "SECRET=1\n",
		"dist/app.js":    "js",
	}, map[string]fakeIgnoreRule{
		".tlc": {source: ".gitignore", line: 3, pattern: ".tlc/"},
		".env": {source: ".gitignore", line: 4, pattern: ".env"},
		"dist": {source: ".gitignore", line: 7, pattern: "dist/"},
	})

	res, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{})
	if err != nil {
		t.Fatalf("CopyIgnored: %v", err)
	}

	for _, rel := range []string{".tlc/db.sqlite", "dist/app.js"} {
		if exists, _ := afero.Exists(fs, filepath.Join(dst, rel)); exists {
			t.Errorf("%s was copied despite its ignore rule carrying %s", rel, IgnoredCopyMarker)
		}
	}
	if exists, _ := afero.Exists(fs, filepath.Join(dst, ".env")); !exists {
		t.Error(".env not copied; an unmarked rule must still copy")
	}

	var marked []string
	for _, s := range res.Skipped {
		if s.Reason == SkipMarked {
			marked = append(marked, s.Path)
		}
	}
	if len(marked) != 2 {
		t.Errorf("Skipped(marked) = %v, want [.tlc dist]", marked)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", res.Warnings)
	}
}

// A marker that is not in the comment block immediately above the pattern
// does nothing: a blank line or a pattern line breaks the association, and
// a marker on the pattern line itself is impossible (git would read it as
// part of the pattern). This pins the "directly above" rule so a stray
// marker elsewhere in the file cannot silently suppress copies.
func TestCopyIgnored_MarkerMustBeDirectlyAbovePattern(t *testing.T) {
	gitignore := strings.Join([]string{
		IgnoredCopyMarker,
		"",
		".env",
		IgnoredCopyMarker,
		"other",
		".tool/",
	}, "\n")
	fs, src, dst, g := markerFixture(t, gitignore, map[string]string{
		".env":       "SECRET=1\n",
		".tool/cfg":  "c",
		"other/file": "o",
	}, map[string]fakeIgnoreRule{
		".env":  {source: ".gitignore", line: 3, pattern: ".env"},
		".tool": {source: ".gitignore", line: 6, pattern: ".tool/"},
		"other": {source: ".gitignore", line: 5, pattern: "other"},
	})

	if _, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{}); err != nil {
		t.Fatalf("CopyIgnored: %v", err)
	}

	for _, rel := range []string{".env", ".tool/cfg"} {
		if exists, _ := afero.Exists(fs, filepath.Join(dst, rel)); !exists {
			t.Errorf("%s not copied; marker separated by a blank/pattern line must not apply", rel)
		}
	}
	if exists, _ := afero.Exists(fs, filepath.Join(dst, "other", "file")); exists {
		t.Error("other/ copied although its rule is directly below a marker")
	}
}

// Ignore rules can come from files other than the root .gitignore: nested
// .gitignore files (relative source path) and the per-repo exclude or the
// user's global excludes file (absolute source path). The marker is read
// from whichever file git names.
func TestCopyIgnored_MarkerHonouredInNestedAndAbsoluteSources(t *testing.T) {
	fs, src, dst := newCopyFixture(t, map[string]string{
		"sub/.gitignore":  IgnoredCopyMarker + "\nscratch/\n",
		"sub/scratch/a":   "a",
		".editor/session": "s",
	})
	global := "/home/u/.config/git/ignore"
	if err := afero.WriteFile(fs, global, []byte("# editor state\n"+IgnoredCopyMarker+"\n.editor/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &fakeIgnoredGit{
		out: "!! sub/scratch/\n!! .editor/\n",
		rules: map[string]fakeIgnoreRule{
			"sub/scratch": {source: "sub/.gitignore", line: 2, pattern: "scratch/"},
			".editor":     {source: global, line: 3, pattern: ".editor/"},
		},
	}

	res, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{})
	if err != nil {
		t.Fatalf("CopyIgnored: %v", err)
	}
	if len(res.Copied) != 0 {
		t.Errorf("Copied = %v, want none", res.Copied)
	}
	if len(res.Skipped) != 2 {
		t.Errorf("Skipped = %v, want 2 marked skips", res.Skipped)
	}
}

// Entries git cannot attribute to a rule — a collapsed directory whose own
// name matches nothing — are copied as before. --non-matching makes git
// print them with empty fields rather than dropping them, so the record
// count stays aligned with the request.
func TestCopyIgnored_UnattributedEntryStillCopied(t *testing.T) {
	fs, src, dst := newCopyFixture(t, map[string]string{"cache/a.log": "l"})
	g := &fakeIgnoredGit{out: "!! cache/\n", rules: map[string]fakeIgnoreRule{}}

	if _, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{}); err != nil {
		t.Fatalf("CopyIgnored: %v", err)
	}
	if exists, _ := afero.Exists(fs, filepath.Join(dst, "cache", "a.log")); !exists {
		t.Error("unattributed entry was not copied")
	}
}

// Marker resolution failing must not turn into a failed copy, but it must
// not be silent either: a user relying on the marker needs to know it was
// not applied.
func TestCopyIgnored_MarkerResolutionFailureWarnsAndCopies(t *testing.T) {
	fs, src, dst := newCopyFixture(t, map[string]string{".env": "S=1\n"})
	g := &fakeIgnoredGit{out: "!! .env\n", checkIgnoreErr: errors.New("boom")}

	res, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{})
	if err != nil {
		t.Fatalf("CopyIgnored: %v", err)
	}
	if exists, _ := afero.Exists(fs, filepath.Join(dst, ".env")); !exists {
		t.Error(".env not copied after marker resolution failure")
	}
	if len(res.Warnings) == 0 || !strings.Contains(res.Warnings[0], IgnoredCopyMarker) {
		t.Errorf("expected a warning naming the marker, got %v", res.Warnings)
	}
}

// Pin the check-ignore invocation: -v is what yields source and line,
// --non-matching keeps one record per path, and --no-index stops a stale
// index entry from hiding a rule. -z must stay absent: git rejects it
// without --stdin, which StatusRunner cannot supply.
func TestCopyIgnored_CheckIgnoreFlags(t *testing.T) {
	fs, src, dst := newCopyFixture(t, map[string]string{".env": "S=1\n"})
	g := &fakeIgnoredGit{out: "!! .env\n", rules: map[string]fakeIgnoreRule{}}

	if _, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{}); err != nil {
		t.Fatalf("CopyIgnored: %v", err)
	}
	joined := strings.Join(g.checkIgnoreArgs, " ")
	for _, f := range []string{"check-ignore", "-v", "--non-matching", "--no-index", "-- .env"} {
		if !strings.Contains(joined, f) {
			t.Errorf("check-ignore args %q missing %q", joined, f)
		}
	}
	if strings.Contains(joined, " -z ") {
		t.Errorf("check-ignore args %q must not pass -z (needs --stdin)", joined)
	}
}

// realGit runs git for real in a directory, so the check-ignore parsing is
// verified against actual git output rather than the fake's rendering.
type realGit struct{ t *testing.T }

func (r realGit) RunInDir(dir string, cmd string, args ...string) (string, error) {
	c := exec.Command(cmd, args...)
	c.Dir = dir
	out, err := c.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), fmt.Errorf("%s: %w: %s", cmd, err, ee.Stderr)
		}
		return string(out), err
	}
	return string(out), nil
}

func TestCopyIgnored_RealGitMarker(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	fs := afero.NewOsFs()
	g := realGit{t: t}
	for _, d := range []string{filepath.Join(src, ".tlc"), filepath.Join(src, "cache"), dst} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := g.RunInDir(src, "git", "init", "-q"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	// Unset global excludes so the assertion depends only on this repo.
	if _, err := g.RunInDir(src, "git", "config", "core.excludesFile", "/dev/null"); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		// "a:b" exercises a pattern containing ':' in the -v record.
		".gitignore":  "# hub-owned\n" + IgnoredCopyMarker + "\n.tlc/\n.env\n*.log\na:b\n",
		".tlc/db":     "d",
		".env":        "S=1\n",
		"cache/a.log": "l", // cache/ collapses to an unattributed directory
		"a:b":         "c",
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(src, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	res, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{})
	if err != nil {
		t.Fatalf("CopyIgnored: %v", err)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings: %v", res.Warnings)
	}
	if _, err := os.Stat(filepath.Join(dst, ".tlc")); !os.IsNotExist(err) {
		t.Errorf(".tlc copied despite marker (stat err=%v)", err)
	}
	for _, rel := range []string{".env", "cache/a.log", "a:b"} {
		if _, err := os.Stat(filepath.Join(dst, rel)); err != nil {
			t.Errorf("%s not copied: %v", rel, err)
		}
	}
}

func TestSplitIgnoreRule(t *testing.T) {
	cases := []struct {
		in     string
		source string
		line   int
		ok     bool
	}{
		{".gitignore:3:.tlc/", ".gitignore", 3, true},
		{"sub/.gitignore:12:a:b", "sub/.gitignore", 12, true},
		{"/Users/u/.config/git/ignore:4:.tlc/*", "/Users/u/.config/git/ignore", 4, true},
		{"C:/Users/u/ignore:7:x", "C:/Users/u/ignore", 7, true},
		{"::", "", 0, false},
		{"garbage", "", 0, false},
	}
	for _, c := range cases {
		source, line, err := splitIgnoreRule(c.in)
		if (err == nil) != c.ok {
			t.Errorf("%q: err=%v, want ok=%v", c.in, err, c.ok)
			continue
		}
		if source != c.source || line != c.line {
			t.Errorf("%q: got (%q,%d), want (%q,%d)", c.in, source, line, c.source, c.line)
		}
	}
}
