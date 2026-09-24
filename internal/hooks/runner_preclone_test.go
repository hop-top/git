package hooks

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pre-clone runs before anything is on disk, so it resolves at hopspace and
// global level only. A .git-hop/hooks/pre-clone at the anchor path or in any
// of its ancestors (the directory the clone runs from, and every directory
// above it) must never be picked up.

const preCloneRepoID = "github.com/test/repo"

func plantHook(t *testing.T, fs afero.Fs, path string) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, afero.WriteFile(fs, path, []byte("#!/bin/sh\nexit 0\n"), 0755))
}

func isolateHookHomes(t *testing.T) (dataHome, configHome string) {
	t.Helper()
	dataHome = filepath.Join("/isolated", "data")
	configHome = filepath.Join("/isolated", "config")
	t.Setenv("GIT_HOP_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	return dataHome, configHome
}

func TestFindHookFile_PreCloneIgnoresAnchorAndAncestors(t *testing.T) {
	isolateHookHomes(t)

	cases := map[string]string{
		"at anchor":       "/work/sub/repo/.git-hop/hooks/pre-clone",
		"in cwd":          "/work/sub/.git-hop/hooks/pre-clone",
		"in cwd ancestor": "/work/.git-hop/hooks/pre-clone",
		"at filesystem /": "/.git-hop/hooks/pre-clone",
	}
	for name, hook := range cases {
		t.Run(name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			plantHook(t, fs, hook)

			found := NewRunner(fs).FindHookFile("pre-clone", "/work/sub/repo", preCloneRepoID)
			assert.Empty(t, found, "pre-clone must not resolve at repo level or via the parent walk")
		})
	}
}

func TestFindHookFile_PreCloneResolvesHopspace(t *testing.T) {
	dataHome, _ := isolateHookHomes(t)
	fs := afero.NewMemMapFs()

	plantHook(t, fs, "/work/.git-hop/hooks/pre-clone")
	hopspace := filepath.Join(dataHome, "github.com", "test", "repo", "hooks", "pre-clone")
	plantHook(t, fs, hopspace)

	found := NewRunner(fs).FindHookFile("pre-clone", "/work/sub/repo", preCloneRepoID)
	assert.Equal(t, hopspace, found)
}

func TestFindHookFile_PreCloneResolvesGlobal(t *testing.T) {
	_, configHome := isolateHookHomes(t)
	fs := afero.NewMemMapFs()

	plantHook(t, fs, "/work/.git-hop/hooks/pre-clone")
	global := filepath.Join(configHome, "git-hop", "hooks", "pre-clone")
	plantHook(t, fs, global)

	found := NewRunner(fs).FindHookFile("pre-clone", "/work/sub/repo", preCloneRepoID)
	assert.Equal(t, global, found)
}

// The restriction is specific to pre-clone: every other hook keeps its
// repo-level lookup and parent walk.
func TestFindHookFile_OtherHooksKeepParentWalk(t *testing.T) {
	isolateHookHomes(t)

	for _, name := range ValidHookNames {
		if name == "pre-clone" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			hook := filepath.Join("/work", ".git-hop", "hooks", name)
			plantHook(t, fs, hook)

			found := NewRunner(fs).FindHookFile(name, "/work/sub/repo", preCloneRepoID)
			assert.Equal(t, hook, found)
		})
	}
}

func TestExecuteHook_PreCloneSkipsAncestorHook(t *testing.T) {
	isolateHookHomes(t)
	fs := afero.NewMemMapFs()
	// A failing ancestor hook: if it resolved, ExecuteHook would try to run
	// it (and fail, since MemMapFs paths do not exist on disk).
	require.NoError(t, fs.MkdirAll("/work/.git-hop/hooks", 0755))
	require.NoError(t, afero.WriteFile(fs, "/work/.git-hop/hooks/pre-clone", []byte("#!/bin/sh\nexit 1\n"), 0755))

	_, err := NewRunner(fs).ExecuteHook("pre-clone", "/work/sub/repo", preCloneRepoID, "")
	assert.NoError(t, err)
}
