package cmd

import "testing"

// TestComposePsHasRunning covers the empty cases for the output of
// `docker compose ps --format json`. Compose emits either an empty JSON
// array ("[]") or an empty string when no containers exist; a naive
// len(s) > 0 check would report true for "[]" and overcount.
func TestComposePsHasRunning(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty string", "", false},
		{"whitespace only", "   \n", false},
		{"empty array", "[]", false},
		{"empty array with whitespace", "  [] \n", false},
		{"array with one entry", `[{"Name":"svc"}]`, true},
		{"array with multiple entries", `[{"Name":"a"},{"Name":"b"}]`, true},
		{"json lines single entry", `{"Name":"svc"}`, true},
		{"json lines multiple entries", `{"Name":"a"}
{"Name":"b"}`, true},
		{"json lines blank lines only", "\n\n\n", false},
		{"malformed non-empty (defensive true)", `garbage`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := composePsHasRunning(tc.in)
			if got != tc.want {
				t.Fatalf("composePsHasRunning(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
