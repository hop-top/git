package e2e

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// jsonRecordKeys parses stderr as exactly one JSON object and returns it
// with its sorted key set.
func jsonRecordKeys(t *testing.T, label, stderr string) (map[string]any, []string) {
	t.Helper()
	line := strings.TrimSuffix(stderr, "\n")
	if strings.Contains(line, "\n") {
		t.Fatalf("%s: stderr has more than one line:\n%s", label, stderr)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("%s: stderr is not a JSON object: %v\n%s", label, err, stderr)
	}
	keys := make([]string, 0, len(rec))
	for k := range rec {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return rec, keys
}

// TestUsageErrorJSON checks that a usage error under a JSON output mode
// reports as the same JSON error record an operation failure emits,
// still with status 129 and without the usage block.
func TestUsageErrorJSON(t *testing.T) {
	t.Parallel()
	env := setupParityEnv(t)

	// The reference shape: an operation failure under --json.
	_, opErr, code := env.run(t, "remove", "--json", "no-such-target")
	if code != 1 {
		t.Fatalf("operation failure exit = %d, want 1 (stderr %q)", code, opErr)
	}
	_, wantKeys := jsonRecordKeys(t, "operation failure", opErr)

	cases := []struct {
		name string
		args []string
		msg  string
	}{
		{"unknown flag", []string{"list", "--json", "--bogus"}, "unknown flag: --bogus"},
		{"unknown flag before --json", []string{"list", "--bogus", "--json"}, "unknown flag: --bogus"},
		{"unknown root flag", []string{"--json", "--bogus"}, "unknown flag: --bogus"},
		{"unknown flag under --format json", []string{"list", "--format=json", "--bogus"}, "unknown flag: --bogus"},
		{"bad arg count", []string{"list", "--json", "extra"}, `unknown command "extra" for "git-hop list"`},
		{"missing arg", []string{"add", "--json"}, "requires a branch name"},
		{"unknown subcommand", []string{"--json", "env", "bogus"}, `unknown command "bogus" for "git-hop env"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := env.run(t, tc.args...)
			if code != 129 {
				t.Errorf("exit = %d, want 129", code)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			rec, keys := jsonRecordKeys(t, strings.Join(tc.args, " "), stderr)
			if !reflect.DeepEqual(keys, wantKeys) {
				t.Errorf("keys = %v, want %v (the operation failure's)", keys, wantKeys)
			}
			if rec["level"] != "error" {
				t.Errorf("level = %v, want error", rec["level"])
			}
			if msg, _ := rec["msg"].(string); !strings.Contains(msg, tc.msg) {
				t.Errorf("msg = %q, want it to contain %q", msg, tc.msg)
			}
		})
	}
}

// TestUsageErrorNonJSONModes keeps the plain git-style line, without the
// usage block, for machine modes that are not JSON: operation failures
// in these modes are plain text too.
func TestUsageErrorNonJSONModes(t *testing.T) {
	t.Parallel()
	env := setupParityEnv(t)
	for _, args := range [][]string{
		{"list", "--porcelain", "--bogus"},
		{"list", "--format=yaml", "--bogus"},
	} {
		stdout, stderr, code := env.run(t, args...)
		if code != 129 || stdout != "" || stderr != "error: unknown flag: --bogus\n" {
			t.Errorf("%v: exit %d, stdout %q, stderr %q; want 129, empty, one error: line",
				args, code, stdout, stderr)
		}
	}
}
