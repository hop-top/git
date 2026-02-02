package hop_test

import (
	"bytes"

	"github.com/spf13/cobra"
)

// executeCommand executes a cobra command with the given arguments
// and returns the combined output and any error
func executeCommand(root *cobra.Command, args ...string) (string, error) {
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)

	err := root.Execute()
	return buf.String(), err
}

// executeCommandSplit executes a cobra command and returns stdout, stderr, and error separately
func executeCommandSplit(root *cobra.Command, args ...string) (stdout, stderr string, err error) {
	outBuf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)

	root.SetOut(outBuf)
	root.SetErr(errBuf)
	root.SetArgs(args)

	err = root.Execute()
	return outBuf.String(), errBuf.String(), err
}

// resetCommand resets a command's state for testing
func resetCommand(cmd *cobra.Command) {
	cmd.SetArgs(nil)
	cmd.SetOut(nil)
	cmd.SetErr(nil)
	cmd.SetIn(nil)
}
