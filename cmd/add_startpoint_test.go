package cmd

import (
	"errors"
	"testing"

	"hop.top/git/test/mocks"
)

func TestPickAddStartPoint_FlagBeatsEnvBeatsConfig(t *testing.T) {
	tests := []struct {
		name              string
		flag, env, config string
		want, wantOrigin  string
	}{
		{"flag wins over env+config", "fromflag", "fromenv", "fromcfg", "fromflag", "--from fromflag"},
		{"flag wins over env", "fromflag", "fromenv", "", "fromflag", "--from fromflag"},
		{"flag wins over config", "fromflag", "", "fromcfg", "fromflag", "--from fromflag"},
		{"env wins over config when no flag", "", "fromenv", "fromcfg", "fromenv", "GIT_HOP_ADD_FROM=fromenv"},
		{"config used when no flag/env", "", "", "fromcfg", "fromcfg", "hop.add.defaultStartPoint=fromcfg"},
		{"all empty -> cwd", "", "", "", "cwd", "hop.add.defaultStartPoint=cwd"},
		{"explicit 'initial' through flag", "initial", "", "default-branch", "initial", "--from initial"},
		{"explicit 'default-branch' through config", "", "", "default-branch", "default-branch", "hop.add.defaultStartPoint=default-branch"},
		{"explicit SHA through env", "", "abc1234", "", "abc1234", "GIT_HOP_ADD_FROM=abc1234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, origin := pickAddStartPoint(tt.flag, tt.env, tt.config)
			if got != tt.want || origin != tt.wantOrigin {
				t.Errorf("pickAddStartPoint(%q,%q,%q) = %q, %q; want %q, %q",
					tt.flag, tt.env, tt.config, got, origin, tt.want, tt.wantOrigin)
			}
		})
	}
}

// originHeadGit is refGit plus a symbolic origin/HEAD.
type originHeadGit struct {
	*refGit
	originHead string
}

func (o *originHeadGit) RunInDir(_ string, _ string, args ...string) (string, error) {
	if len(args) > 0 && args[0] == "symbolic-ref" && o.originHead != "" {
		return o.originHead + "\n", nil
	}
	return "", errors.New("unsupported")
}

func TestResolveAddStartPoint_DefaultBranchKeywords(t *testing.T) {
	tests := []struct {
		name          string
		value         string
		refs          []string
		defaultBranch string
		originHead    string
		want          string
	}{
		{name: "default-branch prefers the local branch", value: "default-branch",
			refs: []string{"refs/heads/main", "refs/remotes/origin/main"}, defaultBranch: "main", want: "refs/heads/main"},
		{name: "default-branch falls back to origin's", value: "default-branch",
			refs: []string{"refs/remotes/origin/main"}, defaultBranch: "main", want: "refs/remotes/origin/main"},
		{name: "default-branch with neither leaves it to the manager", value: "default-branch",
			defaultBranch: "main", want: "default-branch"},
		{name: "default-branch named by origin/HEAD when hop.json has none", value: "default-branch",
			refs: []string{"refs/heads/trunk"}, originHead: "origin/trunk", want: "refs/heads/trunk"},
		{name: "origin-default is the manager's origin-first sentinel", value: "origin-default",
			refs: []string{"refs/heads/main", "refs/remotes/origin/main"}, defaultBranch: "main", want: "default-branch"},
		{name: "initial passes through", value: "initial", defaultBranch: "main", want: "initial"},
		{name: "a ref passes through", value: "feat/x", defaultBranch: "main", want: "feat/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rg := &refGit{MockGit: mocks.NewMockGit(), refs: map[string]bool{}}
			for _, r := range tt.refs {
				rg.refs[r] = true
			}
			g := &originHeadGit{refGit: rg, originHead: tt.originHead}
			def := effectiveDefaultBranch(g, "/hub", tt.defaultBranch)
			sp, err := resolveAddStartPoint(g, "/hub", "/hub", tt.value, "--from "+tt.value, def)
			if err != nil {
				t.Fatal(err)
			}
			if sp.Spec != tt.want {
				t.Errorf("Spec = %q, want %q", sp.Spec, tt.want)
			}
		})
	}
}

func TestDescribeStartPoint_RefForms(t *testing.T) {
	for in, want := range map[string]string{
		"refs/heads/feat/int":      "branch 'feat/int'",
		"refs/heads/main":          "default branch 'main'",
		"refs/remotes/origin/main": "'origin/main'",
		"v1.0":                     "'v1.0'",
	} {
		if got := describeStartPoint(in, "main"); got != want {
			t.Errorf("describeStartPoint(%q) = %q, want %q", in, got, want)
		}
	}
}
