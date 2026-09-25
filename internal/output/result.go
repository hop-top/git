package output

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

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

// ReportOut is where a command writes its human report (summaries,
// plans, layout trees): stdout, or nowhere while the command renders a
// structured result, which stands in for the report and must be all
// stdout carries.
func ReportOut() io.Writer {
	if IsStructured() {
		return io.Discard
	}
	return os.Stdout
}

// DiagOut is where a command writes the detail of a failure it reports
// with Error or Fatal: stdout, as before, for a person at a terminal;
// stderr while a structured result is requested, so stdout stays empty
// and the detail is still seen.
func DiagOut() io.Writer {
	if IsStructured() {
		return os.Stderr
	}
	return os.Stdout
}

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

// resultShapes maps each command with a declared output schema to the
// value the schema was reflected from, so its --cols can be checked
// before the command runs; see ValidateResultCols.
var resultShapes = map[*cobra.Command]any{}

// RegisterResultShape records shape as the result type cmd renders.
func RegisterResultShape(cmd *cobra.Command, shape any) {
	resultShapes[cmd] = shape
}

// ValidateResultCols rejects --cols/--columns naming a column cmd's result
// does not have. kit's Dispatch runs the same check, but only when the
// result renders -- after a mutating command has already done its work --
// so the root runs it in the pre-run instead. Headers come from kit's
// TableHeaders and the message matches Dispatch's.
func ValidateResultCols(cmd *cobra.Command, v *viper.Viper) error {
	shape, ok := resultShapes[cmd]
	if !ok {
		return nil
	}
	cols := requestedCols(cmd, v)
	if len(cols) == 0 {
		return nil
	}
	headers := kitout.TableHeaders(reflect.TypeOf(shape))
	have := make(map[string]struct{}, len(headers))
	for _, h := range headers {
		have[h] = struct{}{}
	}
	for _, c := range cols {
		if _, ok := have[c]; !ok {
			return fmt.Errorf("unknown column %q (valid: %s)", c, strings.Join(headers, ", "))
		}
	}
	return nil
}

// requestedCols merges --cols and --columns the way kit's Dispatch does:
// a flag set on the command line wins over v, and each value may itself
// be comma-separated.
func requestedCols(cmd *cobra.Command, v *viper.Viper) []string {
	var raw []string
	for _, name := range []string{"cols", "columns"} {
		if f := cmd.Flags().Lookup(name); f != nil && f.Changed {
			if s, err := cmd.Flags().GetStringSlice(name); err == nil {
				raw = append(raw, s...)
				continue
			}
		}
		if v != nil {
			raw = append(raw, v.GetStringSlice(name)...)
		}
	}
	var out []string
	for _, item := range raw {
		for _, part := range strings.Split(item, ",") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}
