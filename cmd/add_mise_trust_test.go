package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hop.top/git/internal/output"
)

// captureMiseOutput runs f in human mode with stdout and stderr
// redirected into one buffer, and returns what it printed.
func captureMiseOutput(t *testing.T, f func()) string {
	t.Helper()
	output.SetupLogger(output.ModeHuman, false)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	f()
	os.Stdout, os.Stderr = oldOut, oldErr
	w.Close()
	return <-done
}

// fakeMise answers trust --show from files on disk and a trusted set,
// the way mise does: per directory normally, per file in paranoid mode.
type fakeMise struct {
	missing bool
	trusted map[string]bool // trusted directories
	trusts  []string        // directories Trust was called for
}

// fakeMiseNames are the config names the fake recognises; the real
// list is mise's business.
var fakeMiseNames = []string{"mise.toml", ".mise/config.toml", "mise.local.toml"}

func (f *fakeMise) Available() bool { return !f.missing }

func (f *fakeMise) TrustShow(dir string, paranoid bool) (string, error) {
	var b strings.Builder
	var found bool
	for _, n := range fakeMiseNames {
		p := filepath.Join(dir, n)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		found = true
		if paranoid {
			b.WriteString(p + ": untrusted\n")
		}
	}
	if !found {
		return "No trusted config files found.\n", nil
	}
	if !paranoid {
		status := "untrusted"
		if f.trusted[dir] {
			status = "trusted"
		}
		b.WriteString(dir + ": " + status + "\n")
	}
	// A parent's config, which must be ignored.
	b.WriteString(filepath.Dir(dir) + ": untrusted\n")
	return b.String(), nil
}

func (f *fakeMise) Trust(dir string) error {
	f.trusts = append(f.trusts, dir)
	return nil
}

func writeMise(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTrustWorktreeMise(t *testing.T) {
	const cfg = "[env]\nFOO = \"bar\"\n"
	tests := []struct {
		name        string
		mode        string
		missing     bool
		srcTrusted  bool
		srcFiles    map[string]string
		dstFiles    map[string]string
		noSource    bool
		wantTrusted bool
		wantHint    string
	}{
		{name: "trusted source, same config", mode: miseTrustAuto, srcTrusted: true,
			srcFiles: map[string]string{"mise.toml": cfg}, dstFiles: map[string]string{"mise.toml": cfg}, wantTrusted: true},
		{name: "untrusted source", mode: miseTrustAuto,
			srcFiles: map[string]string{"mise.toml": cfg}, dstFiles: map[string]string{"mise.toml": cfg}, wantHint: "has no trusted mise config"},
		{name: "source without config", mode: miseTrustAuto, srcTrusted: true,
			dstFiles: map[string]string{"mise.toml": cfg}, wantHint: "has no trusted mise config"},
		{name: "content differs", mode: miseTrustAuto, srcTrusted: true,
			srcFiles: map[string]string{"mise.toml": cfg}, dstFiles: map[string]string{"mise.toml": cfg + "BAR = \"1\"\n"}, wantHint: "mise.toml differs"},
		{name: "extra config file in the new worktree", mode: miseTrustAuto, srcTrusted: true,
			srcFiles: map[string]string{"mise.toml": cfg},
			dstFiles: map[string]string{"mise.toml": cfg, ".mise/config.toml": cfg}, wantHint: "are not the source's"},
		{name: "no config in the new worktree", mode: miseTrustAuto, srcTrusted: true,
			srcFiles: map[string]string{"mise.toml": cfg}, wantHint: "no mise config in the new worktree"},
		{name: "mise missing", mode: miseTrustAuto, missing: true, srcTrusted: true,
			srcFiles: map[string]string{"mise.toml": cfg}, dstFiles: map[string]string{"mise.toml": cfg}, wantHint: "mise not found"},
		{name: "no source worktree", mode: miseTrustAuto, noSource: true,
			dstFiles: map[string]string{"mise.toml": cfg}, wantHint: "no source worktree"},
		{name: "never", mode: miseTrustNever, srcTrusted: true,
			srcFiles: map[string]string{"mise.toml": cfg}, dstFiles: map[string]string{"mise.toml": cfg}},
		{name: "always trusts without a trusted source", mode: miseTrustAlways,
			srcFiles: map[string]string{"mise.toml": cfg}, dstFiles: map[string]string{"mise.toml": "different\n"}, wantTrusted: true},
		{name: "always still needs config to trust", mode: miseTrustAlways, wantHint: "no mise config in the new worktree"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
			for _, d := range []string{src, dst} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for n, c := range tt.srcFiles {
				writeMise(t, src, n, c)
			}
			for n, c := range tt.dstFiles {
				writeMise(t, dst, n, c)
			}
			m := &fakeMise{missing: tt.missing, trusted: map[string]bool{src: tt.srcTrusted}}
			source := src
			if tt.noSource {
				source = ""
			}
			stderr := captureMiseOutput(t, func() { trustWorktreeMise(m, tt.mode, source, dst) })

			if got := len(m.trusts) == 1 && m.trusts[0] == dst; got != tt.wantTrusted {
				t.Errorf("trusted dst = %v (calls %v), want %v; stderr:\n%s", got, m.trusts, tt.wantTrusted, stderr)
			}
			if tt.wantHint != "" && !strings.Contains(stderr, tt.wantHint) {
				t.Errorf("stderr lacks %q:\n%s", tt.wantHint, stderr)
			}
			if tt.wantHint != "" && !strings.Contains(stderr, "hint: ") {
				t.Errorf("skip not reported on a hint line:\n%s", stderr)
			}
			if tt.mode == miseTrustNever && stderr != "" {
				t.Errorf("never: want silence, got:\n%s", stderr)
			}
		})
	}
}

func TestMiseTrustMode(t *testing.T) {
	on, off := true, false
	tests := []struct {
		cfg      map[string]string
		override *bool
		want     string
	}{
		{want: miseTrustAuto},
		{cfg: map[string]string{"hop.add.miseTrust": "always"}, want: miseTrustAlways},
		{cfg: map[string]string{"hop.add.miseTrust": "never"}, want: miseTrustNever},
		{cfg: map[string]string{"hop.add.miseTrust": "Never"}, want: miseTrustNever},
		{cfg: map[string]string{"hop.add.miseTrust": "bogus"}, want: miseTrustAuto},
		{cfg: map[string]string{"hop.add.miseTrust": "never"}, override: &on, want: miseTrustAlways},
		{cfg: map[string]string{"hop.add.miseTrust": "always"}, override: &off, want: miseTrustNever},
	}
	for _, tt := range tests {
		if got := miseTrustMode(stubGitConfig(tt.cfg), tt.override); got != tt.want {
			t.Errorf("miseTrustMode(%v, %v) = %q, want %q", tt.cfg, tt.override, got, tt.want)
		}
	}
}
