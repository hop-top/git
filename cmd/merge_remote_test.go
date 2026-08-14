package cmd

import (
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/git/internal/config"
	"hop.top/git/test/mocks"
)

// stubGitConfig returns a *config.GitConfig whose lookups resolve from
// the supplied table instead of the developer's real git config, so
// these tests never depend on ambient `git config hop.*` state.
func stubGitConfig(values map[string]string) *config.GitConfig {
	return &config.GitConfig{
		RunCmd: func(args ...string) (string, error) {
			// args is ["config", "--get", <key>]
			if len(args) == 3 && args[0] == "config" && args[1] == "--get" {
				if v, ok := values[args[2]]; ok {
					return v, nil
				}
			}
			return "", config.ErrKeyNotFound
		},
	}
}

// newMergeFlagSet builds a command carrying a fresh copy of merge's
// flag definitions, so precedence tests exercise the real flag the user
// types rather than a hand-rolled stand-in. Cobra's Changed bit — the
// thing that distinguishes "typed --delete-remote=false" from "never
// mentioned it" — only exists after a real Parse.
//
// The flags are redeclared from mergeCmd's definitions rather than
// shared via AddFlagSet: pflag stores Changed and Value on the *Flag
// itself, so an AddFlagSet'd flag is the same pointer as mergeCmd's and
// one subtest's parse would leak into the next (and into mergeCmd).
func newMergeFlagSet(t *testing.T, argv ...string) *cobra.Command {
	t.Helper()
	c := &cobra.Command{Use: "merge"}
	mergeCmd.Flags().VisitAll(func(f *pflag.Flag) {
		require.Equal(t, "bool", f.Value.Type(),
			"non-bool merge flag %q needs a copy rule here", f.Name)
		c.Flags().Bool(f.Name, f.DefValue == "true", f.Usage)
	})
	require.NoError(t, c.ParseFlags(argv))
	return c
}

// TestMergeDeleteRemote_DefaultStaysLocal pins merge's default
// contract: without --delete-remote and without the config key, the
// merge cleanup path must not touch origin at all. Merge inherited an
// unconditional HasRemoteBranch probe; an unreachable origin stalls
// the command *after* the local merge already landed.
func TestMergeDeleteRemote_DefaultStaysLocal(t *testing.T) {
	mockGit := mocks.NewMockGit()
	// An unreachable origin: if the default path probes, the test fails
	// on the recorded call rather than hanging the suite.
	mockGit.HasRemoteBranchFunc = func(dir, branch string) bool {
		time.Sleep(30 * time.Second)
		return true
	}

	done := make(chan struct{})
	go func() {
		deleteMergedSourceBranch(mockGit, "/base", "feature", false)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("merge cleanup hung: made an unbounded remote call by default")
	}

	assert.Empty(t, mockGit.HasRemoteBranchCalls,
		"default merge must not probe origin; remote deletion is opt-in")
	assert.Empty(t, mockGit.DeletedRemoteBranches,
		"default merge must not delete the remote branch")

	// Local cleanup still happens.
	assert.Contains(t, mockGit.DeletedLocalBranches, "feature")
}

// TestMergeDeleteRemote_DeletesWhenRequested verifies the opt-in path
// probes origin and deletes the merged source branch there.
func TestMergeDeleteRemote_DeletesWhenRequested(t *testing.T) {
	mockGit := mocks.NewMockGit()
	mockGit.RemoteBranchExists = true

	deleteMergedSourceBranch(mockGit, "/base", "feature", true)

	assert.NotEmpty(t, mockGit.HasRemoteBranchCalls,
		"opt-in remote deletion must probe origin first")
	assert.Contains(t, mockGit.DeletedRemoteBranches, "feature")
	assert.Contains(t, mockGit.DeletedLocalBranches, "feature")
}

// TestMergeDeleteRemote_SkipsPushWhenBranchAbsent verifies that with
// remote deletion requested but no matching remote branch, the delete
// push is not attempted.
func TestMergeDeleteRemote_SkipsPushWhenBranchAbsent(t *testing.T) {
	mockGit := mocks.NewMockGit()
	mockGit.RemoteBranchExists = false

	deleteMergedSourceBranch(mockGit, "/base", "feature", true)

	assert.NotEmpty(t, mockGit.HasRemoteBranchCalls)
	assert.Empty(t, mockGit.DeletedRemoteBranches)
}

// TestResolveMergeDeleteRemote covers flag/config precedence for the
// opt-in decision, driving it through merge's real flag set. The flag
// is authoritative when the user actually typed it; the config key
// supplies the default otherwise.
func TestResolveMergeDeleteRemote(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		cfg  map[string]string
		want bool
	}{
		{
			name: "no flag, no config: stays local",
			want: false,
		},
		{
			name: "flag alone turns it on",
			argv: []string{"--delete-remote"},
			want: true,
		},
		{
			name: "config key alone turns it on",
			cfg:  map[string]string{config.KeyMergeDeleteRemote: "true"},
			want: true,
		},
		{
			name: "config key false keeps it off",
			cfg:  map[string]string{config.KeyMergeDeleteRemote: "false"},
			want: false,
		},
		{
			name: "flag beats config: --delete-remote over config false",
			argv: []string{"--delete-remote"},
			cfg:  map[string]string{config.KeyMergeDeleteRemote: "false"},
			want: true,
		},
		{
			name: "flag beats config: --delete-remote=false over config true",
			argv: []string{"--delete-remote=false"},
			cfg:  map[string]string{config.KeyMergeDeleteRemote: "true"},
			want: false,
		},
		{
			name: "git bool spellings are honored",
			cfg:  map[string]string{config.KeyMergeDeleteRemote: "yes"},
			want: true,
		},
		{
			name: "unparseable config value falls back to off",
			cfg:  map[string]string{config.KeyMergeDeleteRemote: "banana"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newMergeFlagSet(t, tt.argv...)
			flagValue, err := c.Flags().GetBool("delete-remote")
			require.NoError(t, err)

			got := resolveMergeDeleteRemote(
				flagValue,
				c.Flags().Changed("delete-remote"),
				stubGitConfig(tt.cfg),
			)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestMergeCmd_HasDeleteRemoteFlag pins the flag onto the merge command
// itself, mirroring remove's flag in name and default.
func TestMergeCmd_HasDeleteRemoteFlag(t *testing.T) {
	f := mergeCmd.Flags().Lookup("delete-remote")
	require.NotNil(t, f, "merge must expose --delete-remote")
	assert.Equal(t, "false", f.DefValue, "merge must stay local by default")
	assert.Equal(t, "bool", f.Value.Type())
}

// TestMergeDeleteRemote_ConfigKeyIsRepoScoped guards the config plumbing
// end to end: the key name merge reads must be the one documented, and
// a real (memory-fs-backed) repo with no such key set must resolve to
// off. Using afero here keeps the test off the developer's real repo.
func TestMergeDeleteRemote_ConfigKeyIsRepoScoped(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/repo", 0o755))

	assert.Equal(t, "hop.merge.deleteRemote", config.KeyMergeDeleteRemote)

	// A config store that knows nothing about the key: merge must stay
	// local rather than inheriting some unrelated default.
	assert.False(t, resolveMergeDeleteRemote(false, false, stubGitConfig(nil)))
}
