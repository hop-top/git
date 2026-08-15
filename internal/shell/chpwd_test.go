package shell_test

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/shell"
)

// Each shell gets the hook mechanism it actually has. Getting this wrong
// fails silently -- an add-zsh-hook line in a bashrc is just an unknown
// command -- so the mechanism is pinned per shell.
func TestGenerateChdirHandler_UsesNativeMechanismPerShell(t *testing.T) {
	cases := []struct {
		shellType string
		want      []string
		reject    []string
	}{
		{
			shellType: "zsh",
			want:      []string{"autoload -U add-zsh-hook", "add-zsh-hook chpwd __git_hop_chdir"},
			reject:    []string{"PROMPT_COMMAND", "--on-variable"},
		},
		{
			// bash has no chpwd, so PROMPT_COMMAND is the only option --
			// which is exactly why bash needs the $PWD guard the others
			// use only for from-state.
			shellType: "bash",
			want:      []string{"PROMPT_COMMAND", "__git_hop_chdir"},
			reject:    []string{"add-zsh-hook", "--on-variable"},
		},
		{
			shellType: "fish",
			want:      []string{"function __git_hop_chdir --on-variable PWD"},
			reject:    []string{"add-zsh-hook", "PROMPT_COMMAND"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.shellType, func(t *testing.T) {
			src := shell.GenerateChdirHandler(tc.shellType)
			for _, w := range tc.want {
				if !strings.Contains(src, w) {
					t.Errorf("%s handler missing %q", tc.shellType, w)
				}
			}
			for _, r := range tc.reject {
				if strings.Contains(src, r) {
					t.Errorf("%s handler carries %q, which belongs to another shell", tc.shellType, r)
				}
			}
		})
	}
}

func TestGenerateChdirHandler_UnknownShellIsEmpty(t *testing.T) {
	if got := shell.GenerateChdirHandler("tcsh"); got != "" {
		t.Errorf("unknown shell produced %q, want empty", got)
	}
}

// The handler must reach the binary only through the hidden subcommand --
// and only once. Any other command in the hot path would be a fork the
// design cannot afford.
func TestGenerateChdirHandler_InvokesOnlyTheHiddenSubcommand(t *testing.T) {
	for _, shellType := range []string{"bash", "zsh", "fish"} {
		src := shell.GenerateChdirHandler(shellType)
		if n := strings.Count(src, shell.NotifyChdirCommand); n != 1 {
			t.Errorf("%s handler references %s %d times, want exactly 1",
				shellType, shell.NotifyChdirCommand, n)
		}
	}
}

// The wrapper block is versioned so existing installations get rewritten
// rather than carrying old code forever while reporting themselves current.
// Adding the chdir handler is precisely such a behaviour change, so a block
// generated before it must read as stale.
func TestWrapperVersion_BumpedForChdirHandler(t *testing.T) {
	for _, shellType := range []string{"bash", "zsh", "fish"} {
		t.Run(shellType, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			rc := "/home/u/.rc"

			// A v2 block: the previous generation, with no chdir handler.
			stale := "# git-hop shell integration (installed by git-hop) v2\n" +
				"git-hop() { :; }\n" +
				"# end git-hop shell integration\n"
			if err := afero.WriteFile(fs, rc, []byte(stale), 0644); err != nil {
				t.Fatal(err)
			}

			if shell.IsWrapperCurrent(fs, rc) {
				t.Fatal("a v2 block still reads as current; the version was not bumped")
			}

			if err := shell.InstallWrapper(fs, shellType, rc); err != nil {
				t.Fatalf("InstallWrapper: %v", err)
			}

			data, err := afero.ReadFile(fs, rc)
			if err != nil {
				t.Fatal(err)
			}
			got := string(data)

			if !strings.Contains(got, "__git_hop_chdir") {
				t.Error("rewritten block does not carry the chdir handler")
			}
			if strings.Contains(got, "v2\n") {
				t.Error("stale v2 block survived the rewrite")
			}
			if !shell.IsWrapperCurrent(fs, rc) {
				t.Error("freshly installed block does not read as current")
			}
		})
	}
}

// The chdir handler ships inside the wrapper block, between its markers, so
// install and uninstall keep treating it as one unit.
func TestGenerateWrapperFunction_EmbedsChdirHandlerInsideMarkers(t *testing.T) {
	for _, shellType := range []string{"bash", "zsh", "fish"} {
		t.Run(shellType, func(t *testing.T) {
			src := shell.GenerateWrapperFunction(shellType)

			idx := strings.Index(src, "__git_hop_chdir")
			if idx == -1 {
				t.Fatal("wrapper block has no chdir handler")
			}
			end := strings.Index(src, "# end git-hop shell integration")
			if end == -1 {
				t.Fatal("wrapper block has no end marker")
			}
			if idx > end {
				t.Error("chdir handler sits outside the block markers; uninstall would strand it")
			}
		})
	}
}
