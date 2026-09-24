package output

import (
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/log/v2"
	"github.com/spf13/viper"
	kitlog "hop.top/kit/go/console/log"
)

type Mode int

const (
	ModeHuman Mode = iota
	ModeJSON
	ModePorcelain
	ModeQuiet
)

var (
	CurrentMode = ModeHuman
	Verbose     = false
	logger      *log.Logger

	// quiet is -q. It is kept apart from CurrentMode because a structured
	// mode outranks ModeQuiet there, yet -q still silences warnings.
	quiet = false
)

func init() {
	// Fallback logger until SetupLogger is called with a real viper.
	logger = log.NewWithOptions(os.Stderr, log.Options{
		Level: log.InfoLevel,
	})
	logger.SetFormatter(log.TextFormatter)
}

// SetupLogger configures the global logger based on mode, verbosity,
// and the kit/log viper-aware constructor.
func SetupLogger(mode Mode, verbose bool) {
	CurrentMode = mode
	Verbose = verbose
	quiet = mode == ModeQuiet

	// Determine desired level from mode + verbose.
	level := log.InfoLevel
	switch mode {
	case ModeQuiet, ModePorcelain:
		level = log.ErrorLevel
	case ModeHuman:
		if verbose {
			level = log.DebugLevel
		}
	case ModeJSON:
		// JSON mode keeps info level; formatter set below.
	}

	logger = newLogger(mode, level)
}

// newLogger builds the logger for mode at level on stderr.
func newLogger(mode Mode, level log.Level) *log.Logger {
	l := kitlog.WithLevel(rootViper(), level)
	if mode == ModeJSON {
		l.SetFormatter(log.JSONFormatter)
	}
	return l
}

// ErrorJSON writes msg to w as the JSON error record Error and Fatal
// emit in JSON mode. It serves errors raised before the output mode is
// set up, such as a usage error, whose command line asked for JSON.
func ErrorJSON(w io.Writer, msg string) {
	l := newLogger(ModeJSON, log.ErrorLevel)
	l.SetOutput(w)
	l.Error(msg)
}

// SetQuiet records -q for a run whose mode is not ModeQuiet, such as
// --porcelain -q or --json -q: warnings and hints are dropped in every
// mode once -q is given. Call it after SetupLogger, which resets it.
func SetQuiet(q bool) { quiet = q || CurrentMode == ModeQuiet }

// rootViper returns the shared viper instance set by SetViper, or a
// zero-value viper if none has been wired yet.
var viperInstance *viper.Viper

// SetViper stores the root viper instance for logger initialisation.
func SetViper(v *viper.Viper) { viperInstance = v }

func rootViper() *viper.Viper {
	if viperInstance != nil {
		return viperInstance
	}
	return viper.New()
}

// GetLogger returns the global logger instance for advanced usage.
func GetLogger() *log.Logger { return logger }

// Fatal prints an error and exits with status 1.
func Fatal(msg string, args ...interface{}) {
	FatalCode(1, msg, args...)
}

// FatalCode prints an error with git's lowercase "fatal:" prefix on
// stderr and exits with the given status. Use it when the porcelain
// convention calls for a code other than 1 — 128 for a fatal git/repo
// error, 129 for a usage error.
func FatalCode(code int, msg string, args ...interface{}) {
	exitWith(code, "fatal", fmt.Sprintf(msg, args...))
}

// ErrorCode prints an error with git's lowercase "error:" prefix on
// stderr and exits with the given status: an operation failure git
// words as an error rather than a fatal. Unlike Error it prints in
// every mode, quiet included, since it is the run's last word.
func ErrorCode(code int, msg string, args ...interface{}) {
	exitWith(code, "error", fmt.Sprintf(msg, args...))
}

func exitWith(code int, prefix, msg string) {
	printError(prefix, msg)
	os.Exit(code)
}

// printError writes an error-level diagnostic to stderr: a JSON record
// in JSON mode, else "<prefix>: msg". Errors are never dropped, in any
// mode or under -q; git's -q is "only report errors".
func printError(prefix, msg string) {
	if CurrentMode == ModeJSON {
		logger.Error(msg)
		return
	}
	fmt.Fprintf(os.Stderr, "%s: %s\n", prefix, msg)
}

// Error prints a non-fatal error with git's lowercase "error:" prefix on
// stderr (a structured record in JSON mode). It prints in every mode,
// quiet included.
func Error(msg string, args ...interface{}) {
	printError("error", fmt.Sprintf(msg, args...))
}

// Warn prints a warning with git's lowercase "warning:" prefix on stderr
// (a structured record in JSON mode). Porcelain and the other structured
// formats keep it, as git keeps warnings beside --porcelain output: it is
// on stderr, so it never mixes into the result on stdout. -q drops it in
// every mode, following git's -q ("only report errors").
func Warn(msg string, args ...interface{}) {
	if quiet {
		return
	}
	formatted := fmt.Sprintf(msg, args...)
	if CurrentMode == ModeJSON {
		logger.Warn(formatted)
		return
	}
	fmt.Fprintf(os.Stderr, "warning: %s\n", formatted)
}

// Hint prints advice with git's lowercase "hint:" prefix on stderr, one
// prefix per line of msg as git's advise() does; an empty line prints a
// bare "hint:". JSON mode emits one structured record (kind=hint).
// Porcelain drops it, and so does -q in every mode.
func Hint(msg string, args ...interface{}) {
	if quiet {
		return
	}
	formatted := fmt.Sprintf(msg, args...)
	switch CurrentMode {
	case ModeJSON:
		logger.Info(formatted, "kind", "hint")
	case ModeHuman:
		var b strings.Builder
		for _, line := range strings.Split(formatted, "\n") {
			if line == "" {
				b.WriteString("hint:\n")
				continue
			}
			b.WriteString("hint: " + line + "\n")
		}
		fmt.Fprint(os.Stderr, b.String())
	}
}

// Note prints unprefixed feedback on stderr, beside the result rather
// than in it, such as a summary of side work. Like Info it is for a
// person at a terminal: every other mode drops it.
func Note(msg string, args ...interface{}) {
	if CurrentMode != ModeHuman {
		return
	}
	fmt.Fprintf(os.Stderr, "%s\n", fmt.Sprintf(msg, args...))
}

// Info prints standard feedback (unless quiet/porcelain/json).
func Info(msg string, args ...interface{}) {
	if CurrentMode != ModeHuman {
		return
	}
	formatted := fmt.Sprintf(msg, args...)
	fmt.Println(formatted)
}

// Debug prints verbose logs.
func Debug(msg string, args ...interface{}) {
	if !Verbose {
		return
	}
	formatted := fmt.Sprintf(msg, args...)
	if CurrentMode == ModeJSON {
		logger.Debug(formatted)
	} else {
		fmt.Fprintf(os.Stderr, "debug: %s\n", formatted)
	}
}

// Success prints a success message with styling.
func Success(msg string, args ...interface{}) {
	if CurrentMode != ModeHuman {
		return
	}
	formatted := fmt.Sprintf(msg, args...)
	fmt.Println(formatted)
}

// WithField returns a logger with a field attached (for structured logging).
func WithField(key string, value interface{}) *log.Logger {
	return logger.With(key, value)
}

// WithFields returns a logger with multiple fields attached.
func WithFields(fields map[string]interface{}) *log.Logger {
	kvs := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		kvs = append(kvs, k, v)
	}
	return logger.With(kvs...)
}

// IsModeHuman returns true if current mode is human-readable.
func IsModeHuman() bool { return CurrentMode == ModeHuman }

// IsVerbose returns true if verbose logging is enabled.
func IsVerbose() bool { return Verbose }
