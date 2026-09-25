package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestChdirChain_WrapperHopPathWithJSON composes the generated wrapper with
// the real binary on a structured switch. The wrapper reads only the exit
// status and asks the binary for its cd target (__current-path); it never
// parses what the switch prints. So a switch that prints its result on
// stdout must still move the shell, and the result must reach the user
// untouched.
func TestChdirChain_WrapperHopPathWithJSON(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	for _, sh := range chainShells {
		t.Run(sh.shellType, func(t *testing.T) {
			t.Parallel()
			bin := lookChainShell(t, sh.bin)

			ce := setupChainEnv(t)
			ce.RunGitHop(t, ce.HubPath, "add", "feature-a")
			integration := ce.integrationBlock(t, sh.shellType)

			body := "git-hop feature-a --json\n" +
				statusEcho(sh.shellType, "WRAPPER_STATUS")

			pwd, out := ce.runProbe(t, bin, sh.shellType, integration,
				body, ce.MainTree)

			if !strings.Contains(out, "WRAPPER_STATUS=0") {
				t.Errorf("%s: wrapper reported non-zero for a successful hop; output:\n%s",
					sh.shellType, out)
			}
			if !strings.Contains(out, `"action": "switched"`) {
				t.Errorf("%s: the switch's JSON result did not reach the user; output:\n%s",
					sh.shellType, out)
			}

			wantPwd := filepath.Join(ce.HubPath, "hops", "feature-a")
			if resolved, err := filepath.EvalSymlinks(wantPwd); err == nil {
				wantPwd = resolved
			}
			if pwd != wantPwd {
				t.Errorf("%s: shell ended at %q, want %q; output:\n%s",
					sh.shellType, pwd, wantPwd, out)
			}
		})
	}
}
