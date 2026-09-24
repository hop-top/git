package hop

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"hop.top/git/internal/config"
	"hop.top/git/internal/git"
)

// performConversion names the bare layout's worktree after the current
// branch. Reached with no branch (the up-front check bypassed), it fails
// with the detached-HEAD error, not a %w wrapping of a nil error.
func TestPerformConversion_DetachedHeadErrorIsReadable(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "proj")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main", repo},
		{"-C", repo, "-c", "user.name=T", "-c", "user.email=t@e.x", "commit", "-q", "--allow-empty", "-m", "one"},
		{"-C", repo, "checkout", "-q", "--detach"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	c := NewConverter(afero.NewOsFs(), git.New())
	err := c.performConversion(repo, true, &config.ConversionResult{})
	if err == nil {
		t.Fatal("conversion of a detached HEAD succeeded")
	}
	if strings.Contains(err.Error(), "%!") {
		t.Errorf("garbled error: %q", err.Error())
	}
	var dhe *DetachedHeadError
	if !errors.As(err, &dhe) {
		t.Errorf("error = %v, want a DetachedHeadError", err)
	}
}
