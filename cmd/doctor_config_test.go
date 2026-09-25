package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hop.top/git/internal/cli"
	"hop.top/git/internal/config"
)

// retiredConfigKeys is every hop.* key git-hop dropped. Listed here rather
// than taken from internal/config, so a key dropped there fails this test.
var retiredConfigKeys = []string{
	"hop.showAllManagedRepos",
	"hop.unusedThresholdDays",
	"hop.conventionWarning",
	"hop.enforceCleanForConversion",
	"hop.conversion.enforceClean",
	"hop.conversion.allowDirtyForce",
	"hop.conversion.autoRollback",
	"hop.backup.enabled",
	"hop.backup.preserveStashes",
	"hop.bareRepo",
	"hop.autoEnvStart",
}

// liveConfigKeys are settings git-hop reads, several sharing a section or a
// name stem with a retired key. doctor must leave them alone.
var liveConfigKeys = map[string]string{
	config.KeyEnvAutoStart:     "true",
	config.KeyBackupMaxBackups: "5",
	"hop.backup.keepBackup":    "true",
	"hop.backup.path":          "/tmp/backups",
	config.KeyGitDomain:        "example.com",
}

type retiredConfigEnv struct {
	t      *testing.T
	hub    string
	global string // the --global config file
}

// newRetiredConfigEnv isolates --global git config and creates a bare
// repository standing in for the current hub.
func newRetiredConfigEnv(t *testing.T) retiredConfigEnv {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(tmp, "gitconfig"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	hub := filepath.Join(tmp, "hub")
	out, err := exec.Command("git", "init", "--quiet", "--bare", hub).CombinedOutput()
	require.NoError(t, err, string(out))
	e := retiredConfigEnv{t: t, hub: hub, global: filepath.Join(tmp, "gitconfig")}
	for k, v := range liveConfigKeys {
		e.set("--global", k, v)
		e.set("--local", k, v)
	}
	return e
}

func (e retiredConfigEnv) git(args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", e.hub}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

// set appends key = value to the scope's config file, the way
// `git config --add` would, without running git.
func (e retiredConfigEnv) set(scope, key, value string) {
	e.t.Helper()
	path := e.global
	if scope == "--local" {
		path = filepath.Join(e.hub, "config")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	require.NoError(e.t, err)
	defer f.Close()
	name := key[strings.LastIndex(key, ".")+1:]
	section := "[hop]"
	if sub := strings.TrimSuffix(strings.TrimPrefix(key, "hop."), "."+name); sub != name {
		section = fmt.Sprintf("[hop %q]", sub)
	}
	_, err = fmt.Fprintf(f, "%s\n\t%s = %q\n", section, name, value)
	require.NoError(e.t, err)
}

// hopKeys returns the scope's hop.* keys and values as git prints them.
func (e retiredConfigEnv) hopKeys(scope string) map[string]string {
	e.t.Helper()
	out, _ := e.git("config", scope, "--get-regexp", `^hop\.`)
	got := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(line, " "); ok {
			got[k] = v
		}
	}
	return got
}

// liveOnly is liveConfigKeys as hopKeys reports them.
func liveOnly() map[string]string {
	want := map[string]string{}
	for k, v := range liveConfigKeys {
		want[strings.ToLower(k)] = v
	}
	return want
}

func (e retiredConfigEnv) get(scope, key string) (string, bool) {
	v, err := e.git("config", scope, "--get", key)
	return v, err == nil
}

func (e retiredConfigEnv) doctor(opts doctorOpts) doctorReport {
	r := doctorReport{records: []doctorRecord{}}
	checkConfig(config.NewGlobalLoader(), e.hub, opts, &r)
	return r
}

// configRecords returns the config check's records of the given kind, by
// subject.
func configRecords(r doctorReport, kind string) map[string][]doctorRecord {
	out := map[string][]doctorRecord{}
	for _, rec := range r.records {
		if rec.Check == doctorCheckConfig && rec.Kind == kind {
			out[rec.Subject] = append(out[rec.Subject], rec)
		}
	}
	return out
}

// doctor warns about every retired key, whatever its value, in --global
// and in the current hub's --local config; the warning names the key and
// the scope. Warnings leave the exit status at 0. --fix --dry-run changes
// nothing; --fix unsets the key in the scope that held it and nothing else.
func TestDoctorConfig_RetiredKeys(t *testing.T) {
	for _, key := range retiredConfigKeys {
		for _, value := range []string{"true", "false", "not-a-bool"} {
			for _, scope := range []string{"--global", "--local"} {
				t.Run(key+"/"+value+"/"+scope, func(t *testing.T) {
					e := newRetiredConfigEnv(t)
					e.set(scope, key, value)

					r := e.doctor(doctorOpts{})
					warnings := configRecords(r, doctorKindWarning)
					require.Len(t, warnings, 1, "config warnings: %+v", r.records)
					require.Len(t, warnings[key], 1, "config warnings: %+v", r.records)
					msg := warnings[key][0].Message
					assert.Contains(t, msg, "retired setting "+key+" in git config "+scope)
					if key == "hop.autoEnvStart" {
						assert.Contains(t, msg, "the setting is now "+config.KeyEnvAutoStart)
					} else {
						assert.Contains(t, msg, "no longer used")
					}
					assert.Empty(t, configRecords(r, doctorKindIssue))
					assert.Equal(t, 0, cli.ExitCode(doctorResult(r)))

					r = e.doctor(doctorOpts{fix: true, dryRun: true})
					assert.Len(t, configRecords(r, doctorKindWouldFix)[key], 1)
					got, ok := e.get(scope, key)
					assert.True(t, ok && got == value, "--dry-run changed %s to %q (set=%v)", key, got, ok)

					r = e.doctor(doctorOpts{fix: true})
					fixed := configRecords(r, doctorKindFixed)
					require.Len(t, fixed[key], 1, "records: %+v", r.records)
					assert.Contains(t, fixed[key][0].Message, "git config "+scope)
					assert.Equal(t, 0, cli.ExitCode(doctorResult(r)))
					for _, s := range []string{"--global", "--local"} {
						assert.Equal(t, liveOnly(), e.hopKeys(s), "%s after --fix", s)
					}

					assert.Empty(t, configRecords(e.doctor(doctorOpts{}), doctorKindWarning))
				})
			}
		}
	}
}

// All retired keys at once, in both scopes: one warning per key and scope,
// and --fix leaves only the live keys behind.
func TestDoctorConfig_RetiredKeysEverywhere(t *testing.T) {
	e := newRetiredConfigEnv(t)
	for i, key := range retiredConfigKeys {
		v := []string{"true", "false", "1"}[i%3]
		e.set("--global", key, v)
		e.set("--local", key, v)
	}

	r := e.doctor(doctorOpts{})
	warnings := configRecords(r, doctorKindWarning)
	assert.Len(t, warnings, len(retiredConfigKeys))
	for _, key := range retiredConfigKeys {
		assert.Len(t, warnings[key], 2, "%s: want one warning per scope", key)
	}
	assert.Equal(t, 0, cli.ExitCode(doctorResult(r)))

	e.doctor(doctorOpts{fix: true})
	for _, scope := range []string{"--global", "--local"} {
		assert.Equal(t, liveOnly(), e.hopKeys(scope), "%s after --fix", scope)
	}
}

// Outside a hub only --global is checked.
func TestDoctorConfig_RetiredKeysNoHub(t *testing.T) {
	e := newRetiredConfigEnv(t)
	e.set("--global", "hop.bareRepo", "true")
	e.set("--local", "hop.backup.enabled", "false")

	r := doctorReport{records: []doctorRecord{}}
	checkConfig(config.NewGlobalLoader(), "", doctorOpts{fix: true}, &r)
	assert.Len(t, configRecords(r, doctorKindFixed)["hop.bareRepo"], 1)
	_, ok := e.get("--local", "hop.backup.enabled")
	assert.True(t, ok, "the hub's --local config was touched without a hub")
}

// A managers.json that does not parse is skipped by every command (with a
// warning), so its managers silently stop applying: doctor reports it as
// an issue naming the file. A valid or absent file reports nothing.
func TestDoctorConfig_BrokenManagersJSON(t *testing.T) {
	e := newRetiredConfigEnv(t)
	assert.Empty(t, configRecords(e.doctor(doctorOpts{}), doctorKindIssue), "no managers.json")

	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "git-hop", "managers.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(`{"packageManagers": []}`), 0o644))
	assert.Empty(t, configRecords(e.doctor(doctorOpts{}), doctorKindIssue), "valid managers.json")

	require.NoError(t, os.WriteFile(path, []byte("{ not json"), 0o644))
	r := e.doctor(doctorOpts{})
	issues := configRecords(r, doctorKindIssue)
	require.Len(t, issues[path], 1, "records: %+v", r.records)
	assert.Contains(t, issues[path][0].Message, "ignored")
	assert.NotEqual(t, 0, cli.ExitCode(doctorResult(r)))
}

// git-hop no longer reads config.json; settings live in git config hop.*.
// A config.json left in the config directory is reported as a warning
// naming the file, with the exit status left at 0. doctor never deletes
// it: --fix and --fix --dry-run leave it, its content, and the record
// kind as they are. No file, no warning.
func TestDoctorConfig_UnusedConfigJSON(t *testing.T) {
	e := newRetiredConfigEnv(t)
	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "git-hop", "config.json")
	assert.Empty(t, configRecords(e.doctor(doctorOpts{}), doctorKindWarning)[path], "no config.json")

	const body = `{"format": "json", "quiet": true}`
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	for _, opts := range []doctorOpts{{}, {fix: true, dryRun: true}, {fix: true}} {
		r := e.doctor(opts)
		warnings := configRecords(r, doctorKindWarning)
		require.Len(t, warnings[path], 1, "opts %+v, records: %+v", opts, r.records)
		msg := warnings[path][0].Message
		assert.Contains(t, msg, "not read")
		assert.Contains(t, msg, "git config hop.*")
		assert.Contains(t, msg, "delete")
		for _, kind := range []string{doctorKindIssue, doctorKindFixed, doctorKindWouldFix, doctorKindFailed} {
			assert.Empty(t, configRecords(r, kind)[path], "opts %+v: %s record for config.json", opts, kind)
		}
		assert.Equal(t, 0, cli.ExitCode(doctorResult(r)), "opts %+v", opts)

		got, err := os.ReadFile(path)
		require.NoError(t, err, "opts %+v removed config.json", opts)
		assert.Equal(t, body, string(got), "opts %+v changed config.json", opts)
	}
}

// git-hop no longer keeps a hub registry in hops.json; state records every
// hub and worktree. A hops.json an earlier release left in the config
// directory is reported as a warning in every mode, and never deleted.
func TestDoctorConfig_UnusedHopsJSON(t *testing.T) {
	e := newRetiredConfigEnv(t)
	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "git-hop", "hops.json")
	assert.Empty(t, configRecords(e.doctor(doctorOpts{}), doctorKindWarning)[path], "no hops.json")

	const body = `{"hops": {"acme/widget:main": {"repo": "acme/widget", "branch": "main", "path": "/hub/hops/main"}}}`
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	for _, opts := range []doctorOpts{{}, {fix: true, dryRun: true}, {fix: true}} {
		r := e.doctor(opts)
		warnings := configRecords(r, doctorKindWarning)
		require.Len(t, warnings[path], 1, "opts %+v, records: %+v", opts, r.records)
		msg := warnings[path][0].Message
		assert.Contains(t, msg, "not used")
		assert.Contains(t, msg, "state")
		assert.Contains(t, msg, "delete")
		for _, kind := range []string{doctorKindIssue, doctorKindFixed, doctorKindWouldFix, doctorKindFailed} {
			assert.Empty(t, configRecords(r, kind)[path], "opts %+v: %s record for hops.json", opts, kind)
		}
		assert.Equal(t, 0, cli.ExitCode(doctorResult(r)), "opts %+v", opts)

		got, err := os.ReadFile(path)
		require.NoError(t, err, "opts %+v removed hops.json", opts)
		assert.Equal(t, body, string(got), "opts %+v changed hops.json", opts)
	}
}
