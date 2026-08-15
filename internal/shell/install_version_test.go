package shell_test

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/hooks"
	"hop.top/git/internal/shell"
)

// legacyBashBlock is the wrapper exactly as it was emitted before the
// navigation directive existed: same marker, no version, no end marker,
// and a completion block trailing the function. This is what is sitting in
// the rc file of every user who installed the old integration.
const legacyBashBlock = `
# git-hop shell integration (installed by git-hop)
git-hop() {
    local should_cd=false
    local first_arg="$1"

    HOP_WRAPPER_ACTIVE=1 command git hop "$@"
    local exit_code=$?
    echo LEGACY_BASH_SENTINEL >/dev/null

    if [[ $exit_code -eq 0 ]] && [[ "$should_cd" = true ]]; then
        cd "$current" || true
    fi

    return $exit_code
}

# git-hop tab completion
_git_hop() {
    local cur prev words cword
    _init_completion -n : || return
}
complete -o default -F _git_hop git-hop
`

const legacyFishBlock = `
# git-hop shell integration (installed by git-hop)
function git-hop
    set -l should_cd false
    env HOP_WRAPPER_ACTIVE=1 command git hop $argv
    set -l exit_code $status
    echo LEGACY_FISH_SENTINEL >/dev/null
    return $exit_code
end

# git-hop tab completion
complete -c git-hop -f -a '(command git-hop __complete (commandline -cop) 2>/dev/null)'
`

// TestInstallWrapper_ReplacesStaleBlock is the deployment test. A user
// carrying the pre-directive wrapper must come away with the current one:
// the old marker had no version, so the install path short-circuited and
// left the buggy block in place forever while reporting success.
func TestInstallWrapper_ReplacesStaleBlock(t *testing.T) {
	for _, tc := range []struct {
		name      string
		shellType string
		legacy    string
		fossil    string
	}{
		{"bash", "bash", legacyBashBlock, "LEGACY_BASH_SENTINEL"},
		{"fish", "fish", legacyFishBlock, "LEGACY_FISH_SENTINEL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			rc := "/home/u/.rc"
			original := "# user config\nalias ll='ls -la'\n" + tc.legacy
			if err := afero.WriteFile(fs, rc, []byte(original), 0644); err != nil {
				t.Fatal(err)
			}

			// Precondition: the stale block is not current.
			if shell.IsWrapperCurrent(fs, rc) {
				t.Fatal("legacy block reported as current; stale detection is broken")
			}

			if err := shell.InstallWrapper(fs, tc.shellType, rc); err != nil {
				t.Fatalf("InstallWrapper: %v", err)
			}

			got := readFile(t, fs, rc)

			if !shell.IsWrapperCurrent(fs, rc) {
				t.Error("wrapper still not current after reinstall")
			}

			// The whole point: the new behaviour must actually be present.
			if !strings.Contains(got, "93") {
				t.Errorf("replaced wrapper lacks the navigation directive check:\n%s", got)
			}

			// And the old code must be gone, not merely shadowed.
			if strings.Contains(got, tc.fossil) {
				t.Errorf("stale wrapper body survived reinstall (found %q):\n%s", tc.fossil, got)
			}

			// Exactly one block, not two stacked generations.
			if n := strings.Count(got, "# git-hop shell integration"); n != 1 {
				t.Errorf("found %d wrapper blocks, want 1:\n%s", n, got)
			}

			// Unrelated user config is untouched.
			if !strings.Contains(got, "alias ll='ls -la'") {
				t.Error("user config was clobbered during rewrite")
			}
		})
	}
}

// TestInstallWrapper_CurrentBlockIsLeftAlone keeps the rewrite path from
// churning a file that is already correct.
func TestInstallWrapper_CurrentBlockIsLeftAlone(t *testing.T) {
	fs := afero.NewMemMapFs()
	rc := "/home/u/.bashrc"

	if err := shell.InstallWrapper(fs, "bash", rc); err != nil {
		t.Fatal(err)
	}
	first := readFile(t, fs, rc)

	for i := 0; i < 3; i++ {
		if err := shell.InstallWrapper(fs, "bash", rc); err != nil {
			t.Fatal(err)
		}
	}
	after := readFile(t, fs, rc)

	if first != after {
		t.Errorf("repeated install mutated an already-current file:\nfirst:\n%s\nafter:\n%s", first, after)
	}
	if n := strings.Count(after, "# git-hop shell integration"); n != 1 {
		t.Errorf("found %d wrapper blocks after repeat installs, want 1", n)
	}
}

// TestInstallUninstall_RoundTripIsClean covers the constraint that
// uninstall must still fully excise whatever install emits -- including
// the tab-completion block, which the old brace-matching walk left behind.
func TestInstallUninstall_RoundTripIsClean(t *testing.T) {
	for _, shellType := range []string{"bash", "zsh", "fish"} {
		t.Run(shellType, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			rc := "/home/u/.rc"
			original := "# user config\nexport EDITOR=vim\nalias g=git\n"
			if err := afero.WriteFile(fs, rc, []byte(original), 0644); err != nil {
				t.Fatal(err)
			}

			if err := shell.InstallWrapper(fs, shellType, rc); err != nil {
				t.Fatal(err)
			}
			if !shell.IsWrapperInstalled(fs, rc) {
				t.Fatal("wrapper not installed")
			}

			if err := shell.UninstallWrapper(fs, rc); err != nil {
				t.Fatalf("UninstallWrapper: %v", err)
			}

			got := readFile(t, fs, rc)

			if shell.IsWrapperInstalled(fs, rc) {
				t.Error("wrapper still detected after uninstall")
			}
			// No orphaned completion block -- the specific defect called out
			// in the old implementation.
			for _, fossil := range []string{
				"git-hop tab completion",
				"complete -o default -F _git_hop",
				"complete -c git-hop",
				"_git_hop",
				"HOP_WRAPPER_ACTIVE",
				"end git-hop shell integration",
			} {
				if strings.Contains(got, fossil) {
					t.Errorf("uninstall left %q behind:\n%s", fossil, got)
				}
			}

			if strings.TrimSpace(got) != strings.TrimSpace(original) {
				t.Errorf("rc file not restored.\n got: %q\nwant: %q", got, original)
			}
		})
	}
}

// TestUninstallWrapper_RemovesLegacyBlockIncludingCompletion pins the
// same cleanliness guarantee for blocks written by the OLD generator,
// which carry no end marker.
func TestUninstallWrapper_RemovesLegacyBlockIncludingCompletion(t *testing.T) {
	for _, tc := range []struct {
		name   string
		legacy string
	}{
		{"bash", legacyBashBlock},
		{"fish", legacyFishBlock},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			rc := "/home/u/.rc"
			original := "# user config\nexport EDITOR=vim\n"
			if err := afero.WriteFile(fs, rc, []byte(original+tc.legacy), 0644); err != nil {
				t.Fatal(err)
			}

			if err := shell.UninstallWrapper(fs, rc); err != nil {
				t.Fatalf("UninstallWrapper: %v", err)
			}

			got := readFile(t, fs, rc)
			for _, fossil := range []string{
				"git-hop tab completion",
				"complete -o default -F _git_hop",
				"complete -c git-hop",
				"HOP_WRAPPER_ACTIVE",
			} {
				if strings.Contains(got, fossil) {
					t.Errorf("legacy uninstall left %q behind:\n%s", fossil, got)
				}
			}
			if strings.TrimSpace(got) != strings.TrimSpace(original) {
				t.Errorf("rc file not restored.\n got: %q\nwant: %q", got, original)
			}
		})
	}
}

// TestGeneratedWrapper_UsesHooksConstant guards against the emitted shell
// drifting from the Go constant it is supposed to mirror.
func TestGeneratedWrapper_UsesHooksConstant(t *testing.T) {
	want := itoa(hooks.ExitNavigationHandled)

	for _, shellType := range []string{"bash", "zsh", "fish"} {
		src := shell.GenerateWrapperFunction(shellType)
		if !strings.Contains(src, want) {
			t.Errorf("%s wrapper does not mention exit code %s:\n%s", shellType, want, src)
		}
	}
}

func readFile(t *testing.T, fs afero.Fs, path string) string {
	t.Helper()
	b, err := afero.ReadFile(fs, path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
