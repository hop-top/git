package output

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	kitout "hop.top/kit/go/console/output"
)

// Structured result state for the running command. Only commands that
// declare an output schema ever get a non-empty format; see SetResultFormat.
var (
	// resultFormat is the key (in kit's --format registry) the result
	// renders in, or "" for the human view.
	resultFormat string
	// resultFormatOpts, when non-nil, replaces --format-opt for the result
	// (--porcelain pins the text format's style this way).
	resultFormatOpts []string
)

// SetResultFormat records the structured format the running command must
// emit its result in, plus --format-opt pairs that override the user's.
// Pass "" for the human view.
func SetResultFormat(format string, formatOpts ...string) {
	resultFormat = format
	resultFormatOpts = formatOpts
}

// ResultFormat returns the structured format set by SetResultFormat.
func ResultFormat() string { return resultFormat }

// IsStructured reports whether the running command emits a structured
// result on stdout instead of its human view. Commands check this to
// skip human-only output (progress lines, tables, hints) and emit their
// result with EmitResult instead.
func IsStructured() bool { return resultFormat != "" }

// IsHumanFormat reports whether format selects the human view: the
// --format default ("table"), "human", or unset.
func IsHumanFormat(format string) bool {
	return format == "" || format == kitout.Table || format == kitout.Human
}

// resultViperKeys are the output settings kit's Dispatch reads from viper
// when the matching flag was not given on the command line.
var resultViperKeys = []string{"format-opt", "cols", "columns", "template", "output"}

// EmitResult renders data to cmd's stdout through kit's output layer, so
// every structured format, --format-opt, --cols, --template and -o behave
// as they do for any other kit command.
//
// Dispatch reads its settings from the flags and, where a flag was not
// set, from a viper. It gets a scratch viper carrying the root's settings
// with the resolved format applied on top: --json and --porcelain choose
// the format without touching --format, and writing that choice into the
// shared root viper would leak it into every later command run in the
// same process.
func EmitResult(cmd *cobra.Command, data any) error {
	root := rootViper()
	v := viper.New()
	for _, key := range resultViperKeys {
		if root.IsSet(key) {
			v.Set(key, root.Get(key))
		}
	}
	v.Set("format", resultFormat)
	if resultFormatOpts != nil {
		v.Set("format-opt", resultFormatOpts)
	}
	return kitout.Dispatch(cmd, v, data)
}
