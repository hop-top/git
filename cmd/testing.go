package cmd

import (
	"bytes"

	"github.com/spf13/cobra"
)

// ExecuteCommand executes a cobra command with the given arguments
// and returns the combined output and any error.
// This is a test helper function shared across cmd tests.
func ExecuteCommand(root *cobra.Command, args ...string) (string, error) {
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)

	err := root.Execute()
	return buf.String(), err
}

// ExecuteCommandSplit executes a cobra command and returns stdout, stderr, and error separately.
// This is a test helper function shared across cmd tests.
func ExecuteCommandSplit(root *cobra.Command, args ...string) (stdout, stderr string, err error) {
	outBuf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)

	root.SetOut(outBuf)
	root.SetErr(errBuf)
	root.SetArgs(args)

	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// ResetCommand resets a command's state for testing.
// This is a test helper function shared across cmd tests.
func ResetCommand(cmd *cobra.Command) {
	cmd.SetArgs(nil)
	cmd.SetOut(nil)
	cmd.SetErr(nil)
	cmd.SetIn(nil)
}
