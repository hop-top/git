package cmd

import (
	"errors"
	"testing"

	"hop.top/git/test/mocks"
)

// refGit answers ref lookups and remote.origin.url from fixed tables.
type refGit struct {
	*mocks.MockGit
	originURL string
	refs      map[string]bool
}

func (r *refGit) RevParse(_ string, args ...string) (string, error) {
	if r.refs[args[len(args)-1]] {
		return "abc", nil
	}
	return "", errors.New("unknown ref")
}

func (r *refGit) GetConfig(_, key string) (string, error) {
	if key == "remote.origin.url" && r.originURL != "" {
		return r.originURL, nil
	}
	return "", errors.New("unset")
}

func TestShouldFetchOrigin(t *testing.T) {
	on, off := true, false
	tests := []struct {
		name       string
		originURL  string
		localRefs  []string
		cfg        map[string]string
		override   *bool
		startPoint string
		want       bool
	}{
		{name: "default branch via origin", originURL: "u", startPoint: "", want: true},
		{name: "default-branch sentinel", originURL: "u", startPoint: "default-branch", want: true},
		{name: "explicit origin/x", originURL: "u", startPoint: "origin/x", want: true},
		{name: "refs/remotes/origin/x", originURL: "u", startPoint: "refs/remotes/origin/x", want: true},
		{name: "local branch", originURL: "u", startPoint: "main", want: false},
		{name: "local branch shadowing origin/x", originURL: "u", localRefs: []string{"refs/heads/origin/x"}, startPoint: "origin/x", want: false},
		{name: "other remote", originURL: "u", startPoint: "upstream/main", want: false},
		{name: "root commit", originURL: "u", startPoint: "initial", want: false},
		{name: "no origin remote", startPoint: "", want: false},
		{name: "config true beats local start-point", originURL: "u", cfg: map[string]string{"hop.add.fetch": "true"}, startPoint: "main", want: true},
		{name: "config false beats origin ref", originURL: "u", cfg: map[string]string{"hop.add.fetch": "false"}, startPoint: "origin/x", want: false},
		{name: "--fetch beats config false", originURL: "u", cfg: map[string]string{"hop.add.fetch": "false"}, override: &on, startPoint: "main", want: true},
		{name: "--no-fetch beats config true", originURL: "u", cfg: map[string]string{"hop.add.fetch": "true"}, override: &off, startPoint: "", want: false},
		{name: "invalid config falls back to auto", originURL: "u", cfg: map[string]string{"hop.add.fetch": "maybe"}, startPoint: "", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &refGit{MockGit: mocks.NewMockGit(), originURL: tt.originURL, refs: map[string]bool{}}
			for _, r := range tt.localRefs {
				g.refs[r] = true
			}
			got := shouldFetchOrigin(g, stubGitConfig(tt.cfg), tt.override, "/hub", tt.startPoint, "main")
			if got != tt.want {
				t.Errorf("shouldFetchOrigin(%q) = %v, want %v", tt.startPoint, got, tt.want)
			}
		})
	}
}
