package services

import (
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/afero"
)

func has(t *testing.T, fs afero.Fs, p string) bool {
	t.Helper()
	ok, err := afero.Exists(fs, p)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

// Tool state dirs never ride along, with no ignore-file marker at all;
// ordinary ignored files still copy.
func TestCopyIgnored_DefaultExcludesToolState(t *testing.T) {
	fs, src, dst := newCopyFixture(t, map[string]string{
		".tlc/db.sqlite":        "x",
		".hop/tlc/config.yaml":  "c",
		".env":                  "S=1\n",
		"sub/.tlc/db.sqlite":    "x",
		"sub/.hop/tlc/cfg.yaml": "c",
		"sub/keep.env":          "k",
	})
	g := &fakeIgnoredGit{out: "!! .env\n!! .hop/\n!! .tlc/\n!! sub/.hop/\n!! sub/.tlc/\n!! sub/keep.env\n"}

	res, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".tlc", ".hop", "sub/.tlc", "sub/.hop"} {
		if has(t, fs, filepath.Join(dst, rel)) {
			t.Errorf("%s copied", rel)
		}
	}
	for _, rel := range []string{".env", "sub/keep.env"} {
		if !has(t, fs, filepath.Join(dst, rel)) {
			t.Errorf("%s not copied", rel)
		}
	}
	var excluded []string
	for _, s := range res.Skipped {
		if s.Reason == SkipExcludedDefault {
			excluded = append(excluded, s.Path)
		}
	}
	sort.Strings(excluded)
	if want := []string{".hop", ".tlc", "sub/.hop", "sub/.tlc"}; !reflect.DeepEqual(excluded, want) {
		t.Errorf("excluded = %v, want %v", excluded, want)
	}
}

// A tool-state dir nested inside an otherwise-copied ignored directory is
// left out too.
func TestCopyIgnored_DefaultExcludesInsideCopiedDir(t *testing.T) {
	fs, src, dst := newCopyFixture(t, map[string]string{
		"scratch/a.txt":      "a",
		"scratch/.tlc/db":    "x",
		"scratch/.hop/c.yml": "c",
	})
	g := &fakeIgnoredGit{out: "!! scratch/\n"}
	if _, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{}); err != nil {
		t.Fatal(err)
	}
	if !has(t, fs, filepath.Join(dst, "scratch/a.txt")) {
		t.Error("scratch/a.txt not copied")
	}
	for _, rel := range []string{"scratch/.tlc", "scratch/.hop"} {
		if has(t, fs, filepath.Join(dst, rel)) {
			t.Errorf("%s copied", rel)
		}
	}
}

func TestCopyIgnored_ConfigExcludeAddsPatterns(t *testing.T) {
	fs, src, dst := newCopyFixture(t, map[string]string{
		".env": "S", "scratch/a": "a", "logs/x.log": "l", "keep/y": "y", "deep/er/cache/z": "z",
	})
	g := &fakeIgnoredGit{out: "!! .env\n!! deep/er/cache/\n!! keep/\n!! logs/\n!! scratch/\n"}
	_, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{
		Exclude: []string{"scratch/", "logs", "deep/er/cache"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"scratch", "logs", "deep/er/cache"} {
		if has(t, fs, filepath.Join(dst, rel)) {
			t.Errorf("%s copied despite config exclude", rel)
		}
	}
	for _, rel := range []string{".env", "keep/y"} {
		if !has(t, fs, filepath.Join(dst, rel)) {
			t.Errorf("%s not copied", rel)
		}
	}
}

// A leading ! removes a built-in default (opt back in).
func TestCopyIgnored_ConfigNegationOptsBackIn(t *testing.T) {
	fs, src, dst := newCopyFixture(t, map[string]string{".tlc/db": "x", ".hop/c": "c"})
	g := &fakeIgnoredGit{out: "!! .hop/\n!! .tlc/\n"}
	if _, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{Exclude: []string{"!.tlc/"}}); err != nil {
		t.Fatal(err)
	}
	if !has(t, fs, filepath.Join(dst, ".tlc/db")) {
		t.Error(".tlc not copied after opt-in")
	}
	if has(t, fs, filepath.Join(dst, ".hop")) {
		t.Error(".hop copied; only .tlc was opted back in")
	}
}

func TestIgnoredCopyExcluder_Patterns(t *testing.T) {
	cases := []struct {
		pats []string
		path string
		want bool
	}{
		{nil, ".tlc", true},
		{nil, "a/b/.hop", true},
		{nil, ".tlc-notes", false},
		{nil, "x.tlc", false},
		{[]string{"*.cache"}, "a/b.cache", true},
		{[]string{"a/b"}, "a/b", true},
		{[]string{"a/b"}, "a/b/c", true},
		{[]string{"a/b"}, "x/a/b", false},
		{[]string{"/a"}, "a", true},
		{[]string{"/a"}, "x/a", false},
		{[]string{"!.tlc", "!.hop"}, ".tlc", false},
		{[]string{"!.tlc"}, ".hop", true},
		{[]string{"", "  "}, ".env", false},
	}
	for _, c := range cases {
		got := NewIgnoredCopyExcluder(c.pats).Excluded(c.path)
		if got != c.want {
			t.Errorf("%v %q = %v, want %v", c.pats, c.path, got, c.want)
		}
	}
}

// The marker mechanism keeps working next to the built-in default and the
// config list: all three skip independently.
func TestCopyIgnored_ComposesWithMarker(t *testing.T) {
	gitignore := strings.Join([]string{"#-hop-#", "private/", ".tlc/", "extra/", ".env"}, "\n")
	fs, src, dst, g := markerFixture(t, gitignore, map[string]string{
		".tlc/db": "x", "private/p": "p", "extra/e": "e", ".env": "S",
	}, map[string]fakeIgnoreRule{
		".tlc":    {source: ".gitignore", line: 3, pattern: ".tlc/"},
		"private": {source: ".gitignore", line: 2, pattern: "private/"},
		"extra":   {source: ".gitignore", line: 4, pattern: "extra/"},
		".env":    {source: ".gitignore", line: 5, pattern: ".env"},
	})
	res, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{Exclude: []string{"extra"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{".tlc", "private", "extra"} {
		if has(t, fs, filepath.Join(dst, rel)) {
			t.Errorf("%s copied", rel)
		}
	}
	if !has(t, fs, filepath.Join(dst, ".env")) {
		t.Error(".env not copied")
	}
	reasons := map[string]SkipReason{}
	for _, s := range res.Skipped {
		reasons[s.Path] = s.Reason
	}
	if reasons["private"] != SkipMarked || reasons[".tlc"] != SkipExcludedDefault || reasons["extra"] != SkipExcludedConfig {
		t.Errorf("reasons = %v", reasons)
	}
}

// A dry run reports exactly the entries the real run copies, and writes
// nothing.
func TestCopyIgnored_DryRunMatchesRun(t *testing.T) {
	files := map[string]string{
		".tlc/db": "x", ".hop/c": "c", ".env": "S", "scratch/a": "a", "keep/k": "k", "big/f": strings.Repeat("z", 64),
	}
	status := "!! .env\n!! .hop/\n!! .tlc/\n!! big/\n!! keep/\n!! scratch/\n"
	opts := IgnoredCopyOptions{MaxBytes: 32, Exclude: []string{"scratch"}}

	fs, src, dst := newCopyFixture(t, files)
	dry := opts
	dry.DryRun = true
	dryRes, err := CopyIgnored(fs, &fakeIgnoredGit{out: status}, src, dst, dry)
	if err != nil {
		t.Fatal(err)
	}
	if has(t, fs, filepath.Join(dst, ".env")) || has(t, fs, filepath.Join(dst, "keep")) {
		t.Fatal("dry run wrote to destination")
	}

	runRes, err := CopyIgnored(fs, &fakeIgnoredGit{out: status}, src, dst, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dryRes.Copied, runRes.Copied) || !reflect.DeepEqual(dryRes.Skipped, runRes.Skipped) {
		t.Errorf("dry %+v != run %+v", dryRes, runRes)
	}
	if want := []string{".env", "keep"}; !reflect.DeepEqual(runRes.Copied, want) {
		t.Errorf("copied = %v, want %v", runRes.Copied, want)
	}
}
