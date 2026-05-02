package hop

import "testing"

func TestActionKind_String(t *testing.T) {
	tests := []struct {
		kind ActionKind
		want string
	}{
		{ActionNoOp, "noop"},
		{ActionRewriteGitdir, "rewrite-gitdir"},
		{ActionRegisterWithGit, "register"},
		{ActionUnregisterFromGit, "unregister"},
		{ActionUpdateHopJSON, "update-hopjson"},
		{ActionKind(99), "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.kind.String(); got != tc.want {
				t.Errorf("ActionKind(%d).String() = %q, want %q", tc.kind, got, tc.want)
			}
		})
	}
}

func TestPlan_HasMutations(t *testing.T) {
	tests := []struct {
		name string
		plan Plan
		want bool
	}{
		{"empty", Plan{}, false},
		{"only noops", Plan{Actions: []Action{{Kind: ActionNoOp}, {Kind: ActionNoOp}}}, false},
		{"one rewrite", Plan{Actions: []Action{{Kind: ActionNoOp}, {Kind: ActionRewriteGitdir}}}, true},
		{"one register", Plan{Actions: []Action{{Kind: ActionRegisterWithGit}}}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.plan.HasMutations(); got != tc.want {
				t.Errorf("HasMutations() = %v, want %v", got, tc.want)
			}
		})
	}
}
