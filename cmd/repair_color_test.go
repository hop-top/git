package cmd

import (
	"testing"

	"hop.top/git/internal/output"
)

// The pre-run's colour setup reads a command's --color only when its
// value is an output.ColorWhen; a plain string flag would be ignored.
func TestRepairColorFlagFeedsColorSetup(t *testing.T) {
	f := repairCmd.Flags().Lookup("color")
	if f == nil {
		t.Fatal("repair has no --color flag")
	}
	if _, ok := f.Value.(*output.ColorWhen); !ok {
		t.Errorf("repair --color value is %T, want *output.ColorWhen", f.Value)
	}
	if f.NoOptDefVal != string(output.ColorAlways) {
		t.Errorf("bare --color = %q, want always", f.NoOptDefVal)
	}
}
