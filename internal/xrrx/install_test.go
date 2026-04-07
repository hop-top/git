package xrrx_test

import (
	"testing"

	"hop.top/git/internal/docker"
	"hop.top/git/internal/git"
	"hop.top/git/internal/xrrx"
)

// resetGlobals undoes any package-level Options InstallFromEnv installs.
// Tests that exercise InstallFromEnv must register this with t.Cleanup
// to avoid leaking state into sibling tests.
func resetGlobals(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		git.SetDefaultOptions()
		docker.SetDefaultOptions()
	})
}

func TestInstallFromEnv_Unset_NoOp(t *testing.T) {
	resetGlobals(t)
	t.Setenv(xrrx.EnvMode, "")
	t.Setenv(xrrx.EnvCassetteDir, "")

	if err := xrrx.InstallFromEnv(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// New() should still produce a RealRunner — defaults are empty.
	g := git.New()
	if _, ok := g.Runner.(*git.RealRunner); !ok {
		t.Fatalf("git default Runner = %T, want *git.RealRunner", g.Runner)
	}
	d := docker.New()
	if _, ok := d.Runner.(*docker.RealRunner); !ok {
		t.Fatalf("docker default Runner = %T, want *docker.RealRunner", d.Runner)
	}
}

func TestInstallFromEnv_Record_WiresXrrRunner(t *testing.T) {
	resetGlobals(t)
	t.Setenv(xrrx.EnvMode, "record")
	t.Setenv(xrrx.EnvCassetteDir, t.TempDir())

	if err := xrrx.InstallFromEnv(); err != nil {
		t.Fatalf("install: %v", err)
	}

	g := git.New()
	if _, ok := g.Runner.(*xrrx.Runner); !ok {
		t.Fatalf("git default Runner = %T, want *xrrx.Runner after InstallFromEnv", g.Runner)
	}
	d := docker.New()
	if _, ok := d.Runner.(*xrrx.Runner); !ok {
		t.Fatalf("docker default Runner = %T, want *xrrx.Runner after InstallFromEnv", d.Runner)
	}
}

func TestInstallFromEnv_ExplicitOptionStillWins(t *testing.T) {
	resetGlobals(t)
	t.Setenv(xrrx.EnvMode, "record")
	t.Setenv(xrrx.EnvCassetteDir, t.TempDir())

	if err := xrrx.InstallFromEnv(); err != nil {
		t.Fatalf("install: %v", err)
	}

	// Explicit WithRunner must override the default.
	custom := &git.RealRunner{}
	g := git.New(git.WithRunner(custom))
	if g.Runner != custom {
		t.Fatalf("explicit WithRunner did not override default; Runner = %T", g.Runner)
	}
}

func TestInstallFromEnv_InvalidMode_ReturnsError(t *testing.T) {
	resetGlobals(t)
	t.Setenv(xrrx.EnvMode, "garbage")
	t.Setenv(xrrx.EnvCassetteDir, t.TempDir())

	if err := xrrx.InstallFromEnv(); err == nil {
		t.Fatal("expected error for invalid XRR_MODE")
	}
}
