package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
)

// fakeIgnoredGit implements StatusRunner, returning canned status output
// and recording the invocation so tests can assert on the exact flags used.
type fakeIgnoredGit struct {
	out     string
	err     error
	lastDir string
	lastCmd []string

	rules           map[string]fakeIgnoreRule
	checkIgnoreErr  error
	checkIgnoreArgs []string
}

func (f *fakeIgnoredGit) RunInDir(dir string, cmd string, args ...string) (string, error) {
	if len(args) > 0 && args[0] == "check-ignore" {
		return f.checkIgnore(args), f.checkIgnoreErr
	}
	f.lastDir = dir
	f.lastCmd = append([]string{cmd}, args...)
	return f.out, f.err
}

// checkIgnore answers `git check-ignore -v --non-matching -- <paths>`.
// Paths present in f.rules are reported as matched by that rule; every
// other path is reported as non-matching (empty source/line/pattern), which
// is what git prints for a collapsed directory whose own name matches no
// pattern. Tests that do not set rules therefore exercise the pre-marker
// behaviour unchanged.
func (f *fakeIgnoredGit) checkIgnore(args []string) string {
	f.checkIgnoreArgs = args
	var paths []string
	for i, a := range args {
		if a == "--" {
			paths = args[i+1:]
			break
		}
	}
	var b strings.Builder
	for _, p := range paths {
		r, ok := f.rules[p]
		if !ok {
			fmt.Fprintf(&b, "::\t%s\n", p)
			continue
		}
		fmt.Fprintf(&b, "%s:%d:%s\t%s\n", r.source, r.line, r.pattern, p)
	}
	return b.String()
}

// fakeIgnoreRule is one `check-ignore -v` record: which ignore file, which
// line of it, and the pattern text at that line.
type fakeIgnoreRule struct {
	source  string
	line    int
	pattern string
}

func TestListIgnoredEntries_ParsesOnlyIgnoredLines(t *testing.T) {
	g := &fakeIgnoredGit{out: strings.Join([]string{
		" M tracked.go",
		"?? untracked.txt",
		"!! .env",
		"!! cache/",
		"!! node_modules/",
		"A  staged.go",
	}, "\n")}

	got, err := ListIgnoredEntries(g, "/src")
	if err != nil {
		t.Fatalf("ListIgnoredEntries: %v", err)
	}

	want := []string{".env", "cache", "node_modules"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// The --ignored mode is a deliberate choice, not an incidental detail:
// `matching` would expand an all-ignored directory into its every file,
// defeating both the size guard and the deps-managed guard, which decide
// per directory entry. Pin the flag so a future edit cannot silently
// switch modes.
func TestListIgnoredEntries_UsesTraditionalMode(t *testing.T) {
	g := &fakeIgnoredGit{out: "!! .env"}

	if _, err := ListIgnoredEntries(g, "/src"); err != nil {
		t.Fatalf("ListIgnoredEntries: %v", err)
	}

	joined := strings.Join(g.lastCmd, " ")
	if !strings.Contains(joined, "--ignored=traditional") {
		t.Errorf("expected --ignored=traditional, got %q", joined)
	}
	// -unormal must be passed EXPLICITLY, not merely left un-overridden.
	// git's traditional mode collapses an ignored directory to one entry
	// only while untracked-files is "normal"; under "all" it expands to
	// every file inside. Both guards decide per entry, so that expansion
	// defeats them: a large directory arrives as many individually
	// under-threshold files, and a deps-managed node_modules arrives as
	// node_modules/... paths that no longer match the managed name.
	//
	// A user with status.showUntrackedFiles=all in their git config hits
	// exactly that, so omitting the flag is not equivalent to passing it —
	// asserting only that "-uall" is absent would let the flag be dropped
	// and still pass. Verified: the command-line flag overrides the config.
	if !strings.Contains(joined, "-unormal") {
		t.Errorf("expected explicit -unormal so status.showUntrackedFiles=all cannot expand ignored dirs per-file, got %q", joined)
	}
	if strings.Contains(joined, "-uall") {
		t.Errorf("-uall forces the per-file expansion traditional avoids: %q", joined)
	}
	if g.lastDir != "/src" {
		t.Errorf("ran in %q, want /src", g.lastDir)
	}
}

func TestListIgnoredEntries_RejectsEscapingPaths(t *testing.T) {
	g := &fakeIgnoredGit{out: strings.Join([]string{
		"!! ../outside",
		"!! /etc/passwd",
		"!! ok.env",
		`!! "quoted path"`,
	}, "\n")}

	got, err := ListIgnoredEntries(g, "/src")
	if err != nil {
		t.Fatalf("ListIgnoredEntries: %v", err)
	}
	if len(got) != 1 || got[0] != "ok.env" {
		t.Errorf("got %v, want only [ok.env]", got)
	}
}

// --- CopyIgnored behaviour ---

// newCopyFixture builds a source worktree on a MemMapFs and returns the fs
// plus the src/dst paths.
func newCopyFixture(t *testing.T, files map[string]string) (afero.Fs, string, string) {
	t.Helper()
	fs := afero.NewMemMapFs()
	src, dst := "/src", "/dst"
	if err := fs.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("mkdir dst: %v", err)
	}
	for rel, content := range files {
		p := filepath.Join(src, rel)
		if err := fs.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := afero.WriteFile(fs, p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	return fs, src, dst
}

func TestCopyIgnored_IgnoredFileAppearsInNewWorktree(t *testing.T) {
	fs, src, dst := newCopyFixture(t, map[string]string{
		".env":                "SECRET=1\n",
		"tracked.go":          "package main\n",
		".tool/settings.json": `{"a":1}`,
	})
	g := &fakeIgnoredGit{out: "!! .env\n!! .tool/\n"}

	res, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{})
	if err != nil {
		t.Fatalf("CopyIgnored: %v", err)
	}

	got, err := afero.ReadFile(fs, filepath.Join(dst, ".env"))
	if err != nil {
		t.Fatalf(".env not copied into new worktree: %v", err)
	}
	if string(got) != "SECRET=1\n" {
		t.Errorf(".env content = %q, want %q", got, "SECRET=1\n")
	}

	// A whole ignored directory arrives with its contents.
	nested, err := afero.ReadFile(fs, filepath.Join(dst, ".tool", "settings.json"))
	if err != nil {
		t.Fatalf(".tool/settings.json not copied: %v", err)
	}
	if string(nested) != `{"a":1}` {
		t.Errorf("nested content = %q", nested)
	}

	if len(res.Copied) != 2 {
		t.Errorf("Copied = %v, want 2 entries", res.Copied)
	}
}

// The mechanism must key off git's ignore list alone. A tracked file is
// git's business — duplicating it here would race the checkout that
// already placed it.
func TestCopyIgnored_TrackedFileNotDuplicated(t *testing.T) {
	fs, src, dst := newCopyFixture(t, map[string]string{
		".env":       "SECRET=1\n",
		"tracked.go": "package main\n",
	})
	g := &fakeIgnoredGit{out: "!! .env\n"}

	if _, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{}); err != nil {
		t.Fatalf("CopyIgnored: %v", err)
	}

	if exists, _ := afero.Exists(fs, filepath.Join(dst, "tracked.go")); exists {
		t.Error("tracked.go was copied by the ignored-file mechanism; " +
			"only entries git reports as ignored may be copied")
	}
}

func TestCopyIgnored_OverThresholdEntrySkippedAndReported(t *testing.T) {
	big := strings.Repeat("x", 2048)
	fs, src, dst := newCopyFixture(t, map[string]string{
		"git-hop": big,
		".env":    "ok\n",
	})
	g := &fakeIgnoredGit{out: "!! git-hop\n!! .env\n"}

	res, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{MaxBytes: 1024})
	if err != nil {
		t.Fatalf("CopyIgnored: %v", err)
	}

	if exists, _ := afero.Exists(fs, filepath.Join(dst, "git-hop")); exists {
		t.Error("over-threshold entry was copied; the size guard did not hold")
	}
	// The under-threshold entry still comes across.
	if exists, _ := afero.Exists(fs, filepath.Join(dst, ".env")); !exists {
		t.Error(".env should still be copied alongside a skipped entry")
	}

	var found *IgnoredSkip
	for i := range res.Skipped {
		if res.Skipped[i].Path == "git-hop" {
			found = &res.Skipped[i]
		}
	}
	if found == nil {
		t.Fatalf("over-threshold entry not reported in Skipped: %+v", res.Skipped)
	}
	if found.Reason != SkipTooLarge {
		t.Errorf("skip reason = %q, want %q", found.Reason, SkipTooLarge)
	}
	if found.Bytes != int64(len(big)) {
		t.Errorf("reported size = %d, want %d", found.Bytes, len(big))
	}
}

// A directory's size is the sum of its files: a build cache made of many
// small files must trip the ceiling as one unit.
func TestCopyIgnored_DirectorySizeIsAggregate(t *testing.T) {
	fs, src, dst := newCopyFixture(t, map[string]string{
		"cache/a": strings.Repeat("a", 600),
		"cache/b": strings.Repeat("b", 600),
	})
	g := &fakeIgnoredGit{out: "!! cache/\n"}

	res, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{MaxBytes: 1000})
	if err != nil {
		t.Fatalf("CopyIgnored: %v", err)
	}

	if exists, _ := afero.Exists(fs, filepath.Join(dst, "cache")); exists {
		t.Error("directory of aggregate 1200B copied under a 1000B ceiling")
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Reason != SkipTooLarge {
		t.Errorf("want one too-large skip, got %+v", res.Skipped)
	}
}

func TestCopyIgnored_DepsManagedPathSkipped(t *testing.T) {
	fs, src, dst := newCopyFixture(t, map[string]string{
		"node_modules/pkg/index.js": "module.exports=1",
		".env":                      "ok\n",
	})
	g := &fakeIgnoredGit{out: "!! node_modules\n!! .env\n"}

	res, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{
		DepsManaged: map[string]bool{"node_modules": true},
	})
	if err != nil {
		t.Fatalf("CopyIgnored: %v", err)
	}

	if exists, _ := afero.Exists(fs, filepath.Join(dst, "node_modules")); exists {
		t.Error("deps-managed node_modules was copied; the deps layer owns " +
			"that path and symlinks it to a shared store")
	}
	if exists, _ := afero.Exists(fs, filepath.Join(dst, ".env")); !exists {
		t.Error(".env should still be copied")
	}

	if len(res.Skipped) != 1 || res.Skipped[0].Reason != SkipDepsManaged {
		t.Errorf("want one deps-managed skip, got %+v", res.Skipped)
	}
}

func TestCopyIgnored_DoesNotOverwriteExistingDestinationFile(t *testing.T) {
	fs, src, dst := newCopyFixture(t, map[string]string{
		".env": "FROM_SOURCE\n",
	})
	if err := afero.WriteFile(fs, filepath.Join(dst, ".env"), []byte("ALREADY_HERE\n"), 0o644); err != nil {
		t.Fatalf("seed dst: %v", err)
	}
	g := &fakeIgnoredGit{out: "!! .env\n"}

	res, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{})
	if err != nil {
		t.Fatalf("CopyIgnored: %v", err)
	}

	got, _ := afero.ReadFile(fs, filepath.Join(dst, ".env"))
	if string(got) != "ALREADY_HERE\n" {
		t.Errorf("destination .env overwritten: got %q, want %q", got, "ALREADY_HERE\n")
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Reason != SkipExists {
		t.Errorf("want one exists skip, got %+v", res.Skipped)
	}
}

// A failure to enumerate entries returns an error for the caller to warn
// about, and never panics or half-writes. cmd/add.go turns this into a
// warning so the create still succeeds.
func TestCopyIgnored_ListFailureIsReportedNotFatal(t *testing.T) {
	fs, src, dst := newCopyFixture(t, map[string]string{".env": "x"})
	g := &fakeIgnoredGit{err: fmt.Errorf("git exploded")}

	res, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{})
	if err == nil {
		t.Fatal("expected an error when the entry list cannot be obtained")
	}
	if res == nil {
		t.Fatal("result must be non-nil even on error so callers can range it")
	}
	if len(res.Copied) != 0 {
		t.Errorf("nothing should have been copied, got %v", res.Copied)
	}
}

// A single unreadable entry degrades to a warning; the remaining entries
// still get copied. Losing one .env must not cost the user the others.
func TestCopyIgnored_PerEntryFailureDoesNotAbortRun(t *testing.T) {
	fs, src, dst := newCopyFixture(t, map[string]string{".env": "ok\n"})
	// "ghost" is reported by git but absent on disk.
	g := &fakeIgnoredGit{out: "!! ghost\n!! .env\n"}

	res, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{})
	if err != nil {
		t.Fatalf("a missing entry must not fail the run: %v", err)
	}
	if exists, _ := afero.Exists(fs, filepath.Join(dst, ".env")); !exists {
		t.Error(".env should still be copied after an earlier entry failed")
	}
	if len(res.Warnings) == 0 {
		t.Error("the unreadable entry should have produced a warning")
	}
}

// --- OsFs-backed tests (MemMapFs has no symlink support) ---

func TestCopyIgnored_PreservesExecBit(t *testing.T) {
	fs := afero.NewOsFs()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	for _, d := range []string{src, dst} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	script := filepath.Join(src, "run.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	// A nested executable inside a copied directory exercises the
	// recursive path, where a mode drop is easiest to miss.
	if err := os.MkdirAll(filepath.Join(src, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	nested := filepath.Join(src, "bin", "tool")
	if err := os.WriteFile(nested, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write nested: %v", err)
	}

	g := &fakeIgnoredGit{out: "!! run.sh\n!! bin/\n"}
	if _, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{}); err != nil {
		t.Fatalf("CopyIgnored: %v", err)
	}

	for _, rel := range []string{"run.sh", filepath.Join("bin", "tool")} {
		info, err := os.Stat(filepath.Join(dst, rel))
		if err != nil {
			t.Fatalf("stat %s: %v", rel, err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s lost its exec bit: mode %v", rel, info.Mode().Perm())
		}
	}
}

// Symlinks are reproduced as symlinks, never followed. Following one that
// points at an ancestor would walk forever.
func TestCopyIgnored_SymlinkCopiedNotFollowed(t *testing.T) {
	fs := afero.NewOsFs()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	for _, d := range []string{src, dst} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// A self-referential link: following it would never terminate.
	loop := filepath.Join(src, "loop")
	if err := os.Symlink(src, loop); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	g := &fakeIgnoredGit{out: "!! loop\n"}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{}); err != nil {
			t.Errorf("CopyIgnored: %v", err)
		}
	}()
	<-done

	info, err := os.Lstat(filepath.Join(dst, "loop"))
	if err != nil {
		t.Fatalf("symlink not reproduced: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink was materialised as a real directory instead of a link")
	}
	target, err := os.Readlink(filepath.Join(dst, "loop"))
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != src {
		t.Errorf("link target = %q, want %q", target, src)
	}
}

// An existing symlink in the destination — the shape the deps layer leaves
// behind — must survive untouched. lexists must not follow it and mistake
// a dangling link for an empty slot.
func TestCopyIgnored_ExistingDestinationSymlinkNotClobbered(t *testing.T) {
	fs := afero.NewOsFs()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	store := filepath.Join(root, "store")
	for _, d := range []string{src, dst, store} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	if err := os.MkdirAll(filepath.Join(src, "node_modules"), 0o755); err != nil {
		t.Fatalf("mkdir src node_modules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "node_modules", "a.js"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	link := filepath.Join(dst, "node_modules")
	if err := os.Symlink(store, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	g := &fakeIgnoredGit{out: "!! node_modules\n"}
	res, err := CopyIgnored(fs, g, src, dst, IgnoredCopyOptions{})
	if err != nil {
		t.Fatalf("CopyIgnored: %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat destination link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("the deps-layer symlink was replaced by a real directory")
	}
	target, _ := os.Readlink(link)
	if target != store {
		t.Errorf("symlink retargeted to %q, want %q", target, store)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Reason != SkipExists {
		t.Errorf("want one exists skip, got %+v", res.Skipped)
	}
}

// --- DepsManagedPaths ---

// The managed set must come from the deps layer's own detection, so a
// custom package manager configured by the user is skipped too. Hardcoding
// names here is exactly the drift this asserts against.
func TestDepsManagedPaths_DerivedFromDetectedPackageManagers(t *testing.T) {
	fs := afero.NewMemMapFs()
	wt := "/wt"
	if err := fs.MkdirAll(wt, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, f := range []string{"package.json", "package-lock.json"} {
		if err := afero.WriteFile(fs, filepath.Join(wt, f), []byte("{}"), 0o644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}

	pms := []PackageManager{
		{
			Name:        "npm",
			DetectFiles: []string{"package.json"},
			LockFiles:   []string{"package-lock.json"},
			DepsDir:     "node_modules",
			InstallCmd:  []string{"npm", "ci"},
		},
		{
			Name:        "cargo",
			DetectFiles: []string{"Cargo.toml"},
			LockFiles:   []string{"Cargo.lock"},
			DepsDir:     "target",
			InstallCmd:  []string{"cargo", "fetch"},
		},
	}
	m := NewDepsManagerFromParts(fs, "/hopspace", &DepsRegistry{}, pms, nil)

	managed := DepsManagedPaths(m, wt)
	if !managed["node_modules"] {
		t.Error("node_modules should be reported as deps-managed")
	}
	if managed["target"] {
		t.Error("cargo is not present in this worktree; target must not be " +
			"reported as managed")
	}
}

func TestDepsManagedPaths_NilManagerYieldsEmptySet(t *testing.T) {
	if got := DepsManagedPaths(nil, "/wt"); len(got) != 0 {
		t.Errorf("want empty set, got %v", got)
	}
}

// A Go worktree that gitignores vendor/ must have vendor/ treated as
// managed, so this feature never copies one worktree's vendor/ over
// another's. deps_manager.go is explicit that vendor/ is the user's source
// of truth and git-hop must not mutate it.
func TestDepsManagedPaths_IncludesGoVendor(t *testing.T) {
	fs := afero.NewMemMapFs()
	wt := "/wt"
	if err := fs.MkdirAll(wt, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, f := range []string{"go.mod", "go.sum"} {
		if err := afero.WriteFile(fs, filepath.Join(wt, f), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}

	pms := []PackageManager{{
		Name:        "go",
		DetectFiles: []string{"go.mod"},
		LockFiles:   []string{"go.sum"},
		DepsDir:     "vendor",
		InstallCmd:  []string{"go", "mod", "download"},
	}}
	m := NewDepsManagerFromParts(fs, "/hopspace", &DepsRegistry{}, pms, nil)

	if !DepsManagedPaths(m, wt)["vendor"] {
		t.Error("Go vendor/ must be treated as deps-managed so this feature " +
			"never copies or overwrites it")
	}
}
