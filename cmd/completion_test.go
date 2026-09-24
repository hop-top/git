package cmd

import "testing"

func TestCompletionArgs_RefusesUnsupportedShell(t *testing.T) {
	for _, shell := range completionCmd.ValidArgs {
		if err := completionCmd.ValidateArgs([]string{shell}); err != nil {
			t.Errorf("%s: refused: %v", shell, err)
		}
	}
	err := completionCmd.ValidateArgs([]string{"bogus"})
	if err == nil {
		t.Fatal("bogus: accepted, want refusal")
	}
	if want := `unsupported shell "bogus" (valid: bash, zsh, fish)`; err.Error() != want {
		t.Errorf("bogus: error %q, want %q", err, want)
	}
}
