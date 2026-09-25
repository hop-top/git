package hop_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/git"
	"hop.top/git/internal/hop"
)

// linkedZoo is a regular repository with one linked worktree of each kind
// a bare conversion carries, keyed by a short name, at its path before
// the conversion.
type linkedZoo struct {
	root, repo string
	wt         map[string]string
	// stagedBlob is a blob only the dirty worktree's index names.
	stagedBlob string
}

func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func newLinkedRepo(t *testing.T) (root, repo string) {
	t.Helper()
	root = realTempDir(t)
	repo = filepath.Join(root, "proj")
	mustRun(t, "git", "init", "-q", "-b", "main", repo)
	mustRun(t, "git", "-C", repo, "config", "user.name", "T")
	mustRun(t, "git", "-C", repo, "config", "user.email", "t@e.x")
	writeFile(t, filepath.Join(repo, "a.txt"), "a\n", 0o644)
	writeFile(t, filepath.Join(repo, "sub", "s.txt"), "s\n", 0o644)
	writeFile(t, filepath.Join(repo, ".gitignore"), ".worktrees/\n", 0o644)
	mustRun(t, "git", "-C", repo, "add", ".")
	mustRun(t, "git", "-C", repo, "commit", "-q", "-m", "init")
	return root, repo
}

func buildLinkedZoo(t *testing.T) *linkedZoo {
	t.Helper()
	root, repo := newLinkedRepo(t)
	z := &linkedZoo{root: root, repo: repo, wt: map[string]string{
		"dirty":          filepath.Join(root, "ext", "dirty"),
		"locked":         filepath.Join(root, "ext", "locked"),
		"renamed":        filepath.Join(root, "ext", "somedir"),
		"nested":         filepath.Join(repo, ".worktrees", "d"),
		"clash":          filepath.Join(root, "other", "main"),
		"sparse":         filepath.Join(root, "ext", "sparse"),
		"detached":       filepath.Join(root, "ext", "detached"),
		"nestedDetached": filepath.Join(repo, "wts", "det"),
	}}
	add := func(path string, args ...string) {
		mustRun(t, "git", append([]string{"-C", repo, "worktree", "add", "-q", path}, args...)...)
	}
	add(z.wt["dirty"], "-b", "feat-a")
	add(z.wt["locked"], "-b", "feat-b")
	add(z.wt["renamed"], "-b", "feat/c")
	add(z.wt["nested"], "-b", "feat-d")
	add(z.wt["clash"], "-b", "feat-e")
	add(z.wt["sparse"], "-b", "feat-f")
	add(z.wt["detached"], "--detach")
	add(z.wt["nestedDetached"], "--detach")

	d := z.wt["dirty"]
	writeFile(t, filepath.Join(d, "new.txt"), "staged only\n", 0o644)
	mustRun(t, "git", "-C", d, "add", "new.txt")
	writeFile(t, filepath.Join(d, "a.txt"), "a staged\n", 0o644)
	mustRun(t, "git", "-C", d, "add", "a.txt")
	writeFile(t, filepath.Join(d, "a.txt"), "a unstaged\n", 0o644)
	writeFile(t, filepath.Join(d, "untracked.txt"), "u\n", 0o644)
	writeFile(t, filepath.Join(d, "ita.txt"), "ita\n", 0o644)
	mustRun(t, "git", "-C", d, "add", "-N", "ita.txt")
	mustRun(t, "git", "-C", d, "update-ref", "refs/worktree/mark", "HEAD")
	mustRun(t, "git", "-C", d, "config", "extensions.worktreeConfig", "true")
	mustRun(t, "git", "-C", d, "config", "--worktree", "hop.test.own", "dirty-only")
	z.stagedBlob = strings.Fields(gitOut(t, "-C", d, "ls-files", "-s", "new.txt"))[1]

	mustRun(t, "git", "-C", repo, "worktree", "lock", "--reason", "on usb disk", z.wt["locked"])
	mustRun(t, "git", "-C", z.wt["sparse"], "sparse-checkout", "set", "sub")
	return z
}

// wtView is what a user sees of one worktree: its HEAD, index and
// working tree, per-worktree refs and config, and sparse patterns.
func wtView(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	for _, args := range [][]string{
		{"rev-parse", "--symbolic-full-name", "HEAD"},
		{"rev-parse", "HEAD"},
		{"status", "--porcelain=v2", "--untracked-files=all"},
		{"diff", "--no-ext-diff", "--cached", "--binary"},
		{"diff", "--no-ext-diff", "--binary"},
		{"for-each-ref", "refs/worktree", "refs/bisect"},
		{"config", "--worktree", "--list"},
		{"sparse-checkout", "list"},
	} {
		// stdout only: git's errors name the git dir, which moves.
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
		b.WriteString("$ git " + strings.Join(args, " ") + "\n" + string(out))
		if err != nil {
			b.WriteString("(" + err.Error() + ")\n")
		}
	}
	return b.String()
}

func zooViews(t *testing.T, paths map[string]string) map[string]string {
	t.Helper()
	views := map[string]string{}
	for name, p := range paths {
		views[name] = wtView(t, p)
	}
	return views
}

func convertBare(t *testing.T, repo string, g git.GitInterface) (*hop.Converter, error) {
	t.Helper()
	conv := hop.NewConverter(afero.NewOsFs(), g)
	conv.BackupRoot = filepath.Join(filepath.Dir(repo), "backups")
	_, err := conv.ConvertToBareWorktree(repo, true, true)
	return conv, err
}

func absGitDir(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(gitOut(t, "-C", dir, "rev-parse", "--absolute-git-dir"))
}

// A bare conversion carries every linked worktree into the hub: those
// outside the repository stay where they are, those inside its working
// tree move to hops/<branch> (hops/<id> when detached), and each keeps
// its index, per-worktree refs and config, sparse patterns and lock.
func TestConvertBare_CarriesLinkedWorktrees(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	z := buildLinkedZoo(t)
	before := zooViews(t, z.wt)

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	conv.BackupRoot = filepath.Join(z.root, "backups")
	result, err := conv.ConvertToBareWorktree(z.repo, true, true)
	if err != nil {
		t.Fatalf("conversion failed: %v (%v)", err, result.Errors)
	}

	after := map[string]string{}
	for name, p := range z.wt {
		after[name] = p
	}
	after["nested"] = filepath.Join(z.repo, "hops", "feat-d")
	after["nestedDetached"] = filepath.Join(z.repo, "hops", "det")

	for name, p := range after {
		if got := wtView(t, p); got != before[name] {
			t.Errorf("%s (%s) differs after the conversion\ngot:\n%s\nwant:\n%s", name, p, got, before[name])
		}
		if got, want := absGitDir(t, p), filepath.Join(z.repo, "worktrees"); filepath.Dir(got) != want {
			t.Errorf("%s: git dir %s, want one under %s", name, got, want)
		}
	}

	list := gitOut(t, "-C", z.repo, "worktree", "list", "--porcelain")
	if strings.Contains(list, "prunable") {
		t.Errorf("worktree list reports prunable entries:\n%s", list)
	}
	if n := strings.Count(list, "worktree "); n != 10 {
		t.Errorf("worktree list has %d entries, want 10 (hub, default, 8 carried):\n%s", n, list)
	}

	// Ids are kept: the clashing worktree keeps "main", the default
	// worktree takes another id.
	if got := absGitDir(t, after["clash"]); filepath.Base(got) != "main" {
		t.Errorf("clashing worktree's git dir = %s, want its id main kept", got)
	}
	if got := absGitDir(t, filepath.Join(z.repo, "hops", "main")); filepath.Base(got) == "main" {
		t.Errorf("default worktree took the carried id main: %s", got)
	}

	reason, err := os.ReadFile(filepath.Join(z.repo, "worktrees", "locked", "locked"))
	if err != nil || strings.TrimSpace(string(reason)) != "on usb disk" {
		t.Errorf("lock not carried: %q, %v", reason, err)
	}

	for _, gone := range []string{".worktrees", "wts"} {
		if _, err := os.Stat(filepath.Join(z.repo, "hops", "main", gone)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("hops/main/%s left behind after its worktree moved out (err=%v)", gone, err)
		}
	}

	mustRun(t, "git", "-C", z.repo, "gc", "-q", "--prune=now")
	mustRun(t, "git", "-C", z.wt["dirty"], "cat-file", "-e", z.stagedBlob)

	var cfg struct {
		Repo struct {
			DefaultBranch string `json:"defaultBranch"`
		} `json:"repo"`
		Branches map[string]struct {
			Path string `json:"path"`
		} `json:"branches"`
	}
	raw, err := os.ReadFile(filepath.Join(z.repo, "hop.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	wantBranches := map[string]string{
		"main":   filepath.Join("hops", "main"),
		"feat-a": z.wt["dirty"],
		"feat-b": z.wt["locked"],
		"feat/c": z.wt["renamed"],
		"feat-d": filepath.Join("hops", "feat-d"),
		"feat-e": z.wt["clash"],
		"feat-f": z.wt["sparse"],
	}
	if len(cfg.Branches) != len(wantBranches) {
		t.Errorf("hop.json branches = %v, want %v", cfg.Branches, wantBranches)
	}
	for b, p := range wantBranches {
		if cfg.Branches[b].Path != p {
			t.Errorf("hop.json branches[%s].path = %q, want %q", b, cfg.Branches[b].Path, p)
		}
	}
	if cfg.Repo.DefaultBranch != "main" {
		t.Errorf("hop.json defaultBranch = %q, want main", cfg.Repo.DefaultBranch)
	}

	for _, w := range result.Warnings {
		if strings.Contains(w, "failed to create worktree") {
			t.Errorf("the conversion tried to add a carried branch again: %s", w)
		}
	}

	if len(result.Carried) != 8 {
		t.Errorf("result.Carried = %v, want 8 entries", result.Carried)
	}
}

// A submodule checked out in a linked worktree keeps working: its git dir
// moves with the worktree's admin dir, one level up, and both relative
// links between them are rewritten.
func TestConvertBare_CarriesLinkedWorktreeSubmodules(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	root, repo := newLinkedRepo(t)
	lib := filepath.Join(root, "lib")
	mustRun(t, "git", "init", "-q", "-b", "main", lib)
	writeFile(t, filepath.Join(lib, "l.txt"), "l\n", 0o644)
	mustRun(t, "git", "-C", lib, "add", ".")
	mustRun(t, "git", "-C", lib, "-c", "user.name=T", "-c", "user.email=t@e.x", "commit", "-q", "-m", "l")
	mustRun(t, "git", "-C", repo, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "lib")
	mustRun(t, "git", "-C", repo, "commit", "-q", "-m", "sub")

	ext := filepath.Join(root, "ext", "wt")
	nested := filepath.Join(repo, ".worktrees", "n")
	mustRun(t, "git", "-C", repo, "worktree", "add", "-q", ext, "-b", "feat")
	mustRun(t, "git", "-C", repo, "worktree", "add", "-q", nested, "-b", "feat-n")
	for _, wt := range []string{ext, nested} {
		mustRun(t, "git", "-C", wt, "-c", "protocol.file.allow=always", "submodule", "update", "--init", "-q")
	}

	if _, err := convertBare(t, repo, git.New()); err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	for _, wt := range []string{ext, filepath.Join(repo, "hops", "feat-n")} {
		sub := filepath.Join(wt, "lib")
		want := filepath.Join(absGitDir(t, wt), "modules", "lib")
		if got := absGitDir(t, sub); got != want {
			t.Errorf("%s: submodule git dir = %s, want %s", sub, got, want)
		}
		if got := strings.TrimSpace(gitOut(t, "-C", sub, "rev-parse", "--show-toplevel")); got != sub {
			t.Errorf("%s: submodule work tree = %s", sub, got)
		}
		if out := gitOut(t, "-C", sub, "status", "--porcelain"); out != "" {
			t.Errorf("%s: submodule not clean after the conversion:\n%s", sub, out)
		}
	}
}

// A shallow repository is cloned through the transport, which copies only
// what a ref reaches; a blob only a linked worktree's index names must be
// carried all the same.
func TestConvertBare_ShallowCarriesLinkedIndexObjects(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	root, upstream := newLinkedRepo(t)
	for _, c := range []string{"two", "three"} {
		writeFile(t, filepath.Join(upstream, "a.txt"), c+"\n", 0o644)
		mustRun(t, "git", "-C", upstream, "commit", "-q", "-am", c)
	}
	repo := filepath.Join(root, "shallow")
	mustRun(t, "git", "clone", "-q", "--depth", "1", "file://"+upstream, repo)
	mustRun(t, "git", "-C", repo, "config", "user.name", "T")
	mustRun(t, "git", "-C", repo, "config", "user.email", "t@e.x")
	wt := filepath.Join(root, "ext", "wt")
	mustRun(t, "git", "-C", repo, "worktree", "add", "-q", wt, "-b", "feat")
	writeFile(t, filepath.Join(wt, "staged.txt"), "only in the index\n", 0o644)
	mustRun(t, "git", "-C", wt, "add", "staged.txt")
	blob := strings.Fields(gitOut(t, "-C", wt, "ls-files", "-s", "staged.txt"))[1]
	before := wtView(t, wt)

	if _, err := convertBare(t, repo, git.New()); err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	mustRun(t, "git", "-C", repo, "cat-file", "-e", blob)
	if got := wtView(t, wt); got != before {
		t.Errorf("linked worktree differs\ngot:\n%s\nwant:\n%s", got, before)
	}
}

// A linked worktree that is gone and unlocked is what git calls
// prunable: left behind, with a warning. One that is locked but absent
// (an unmounted disk) is carried, and a sweep repair from the hub
// reconnects it once it is back.
func TestConvertBare_LinkedMissingAndPrunable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	root, repo := newLinkedRepo(t)
	gone := filepath.Join(root, "ext", "gone")
	usb := filepath.Join(root, "ext", "usb")
	mustRun(t, "git", "-C", repo, "worktree", "add", "-q", gone, "-b", "gone")
	mustRun(t, "git", "-C", repo, "worktree", "add", "-q", usb, "-b", "usb")
	mustRun(t, "git", "-C", repo, "worktree", "lock", usb)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	unplugged := filepath.Join(root, "unplugged")
	if err := os.Rename(usb, unplugged); err != nil {
		t.Fatal(err)
	}

	conv := hop.NewConverter(afero.NewOsFs(), git.New())
	conv.BackupRoot = filepath.Join(root, "backups")
	result, err := conv.ConvertToBareWorktree(repo, true, true)
	if err != nil {
		t.Fatalf("conversion failed: %v (%v)", err, result.Errors)
	}
	warnings := strings.Join(result.Warnings, "\n")
	if !strings.Contains(warnings, gone) || !strings.Contains(warnings, "prunable") {
		t.Errorf("no warning for the prunable worktree %s:\n%s", gone, warnings)
	}
	if !strings.Contains(warnings, usb) || !strings.Contains(warnings, "worktree repair") {
		t.Errorf("no repair hint for the absent locked worktree %s:\n%s", usb, warnings)
	}
	if _, err := os.Stat(filepath.Join(repo, "worktrees", "gone")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("prunable admin dir carried (err=%v)", err)
	}

	if err := os.Rename(unplugged, usb); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "git", "-C", repo, "worktree", "repair")
	if got, want := absGitDir(t, usb), filepath.Join(repo, "worktrees", "usb"); got != want {
		t.Errorf("remounted worktree's git dir = %s, want %s", got, want)
	}
}

// failVerify is real git whose rev-parse in one linked worktree reports a
// git dir other than the hub's, standing in for a link the conversion
// did not rewrite.
type failVerify struct {
	git.GitInterface
	wt string
}

func (f failVerify) Run(cmd string, args ...string) (string, error) {
	if len(args) > 2 && args[0] == "-C" && args[1] == f.wt && strings.Contains(strings.Join(args, " "), "--absolute-git-dir") {
		return "/nowhere", nil
	}
	return f.GitInterface.Run(cmd, args...)
}

// A carried worktree that does not reach the hub afterwards fails the
// conversion, and the rollback leaves the repository and every linked
// worktree as they were.
func TestConvertBare_LinkedVerifyFailureRollsBack(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	z := buildLinkedZoo(t)
	before := zooViews(t, z.wt)

	_, err := convertBare(t, z.repo, failVerify{GitInterface: git.New(), wt: z.wt["dirty"]})
	if err == nil {
		t.Fatal("conversion succeeded, want the verify failure")
	}
	if !strings.Contains(err.Error(), z.wt["dirty"]) {
		t.Errorf("error %q does not name the worktree that failed", err)
	}
	if got := strings.TrimSpace(gitOut(t, "-C", z.repo, "rev-parse", "--is-bare-repository")); got != "false" {
		t.Errorf("repository not restored: bare=%s", got)
	}
	for name, p := range z.wt {
		if got := wtView(t, p); got != before[name] {
			t.Errorf("%s differs after the rollback\ngot:\n%s\nwant:\n%s", name, got, before[name])
		}
		if got, want := filepath.Dir(absGitDir(t, p)), filepath.Join(z.repo, ".git", "worktrees"); got != want {
			t.Errorf("%s: git dir under %s after the rollback, want %s", name, got, want)
		}
	}
}

// observeAdd is real git that records, when the hub's default worktree
// is added, whether a linked worktree's admin dir is in the old .git and
// in the hub.
type observeAdd struct {
	git.GitInterface
	oldAdmin, newAdmin string
	sawOld, sawNew     *bool
}

func (o observeAdd) Run(cmd string, args ...string) (string, error) {
	if len(args) > 3 && args[2] == "worktree" && args[3] == "add" {
		_, errOld := os.Stat(o.oldAdmin)
		_, errNew := os.Stat(o.newAdmin)
		*o.sawOld, *o.sawNew = errOld == nil, errNew == nil
	}
	return o.GitInterface.Run(cmd, args...)
}

// The admin dirs are copied into the hub, not moved, and before the
// default worktree is added: until the swap, the linked worktrees keep
// working from the old .git, and the carried ids are taken first.
func TestConvertBare_CopiesAdminDirsBeforeDefaultAdd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	root, repo := newLinkedRepo(t)
	wt := filepath.Join(root, "ext", "wt")
	mustRun(t, "git", "-C", repo, "worktree", "add", "-q", wt, "-b", "feat")

	var sawOld, sawNew bool
	g := observeAdd{GitInterface: git.New(),
		oldAdmin: filepath.Join(repo, ".git", "worktrees", "wt"),
		newAdmin: filepath.Join(repo+".new", "worktrees", "wt"),
		sawOld:   &sawOld, sawNew: &sawNew}
	if _, err := convertBare(t, repo, g); err != nil {
		t.Fatalf("conversion failed: %v", err)
	}
	if !sawNew {
		t.Error("admin dir not in the hub when the default worktree was added")
	}
	if !sawOld {
		t.Error("admin dir gone from the old .git before the swap: moved, not copied")
	}
}
