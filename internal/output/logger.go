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
	formatted := fmt.Sprintf(msg, args...)
	if CurrentMode == ModeJSON {
		logger.Error(formatted)
	} else {
		fmt.Fprintf(os.Stderr, "fatal: %s\n", formatted)
	}
	os.Exit(code)
}

// Error prints a non-fatal error.
func Error(msg string, args ...interface{}) {
	if CurrentMode == ModeQuiet {
		return
	}
	formatted := fmt.Sprintf(msg, args...)
	if CurrentMode == ModeJSON {
		logger.Error(formatted)
	} else {
		fmt.Fprintf(os.Stderr, "error: %s\n", formatted)
	}
}

// Warn prints a warning with git's lowercase "warning:" prefix on stderr
// (a structured record in JSON mode). Quiet and porcelain modes drop it.
func Warn(msg string, args ...interface{}) {
	formatted := fmt.Sprintf(msg, args...)
	switch CurrentMode {
	case ModeJSON:
		logger.Warn(formatted)
	case ModeHuman:
		fmt.Fprintf(os.Stderr, "warning: %s\n", formatted)
	}
}

// Hint prints advice with git's lowercase "hint:" prefix on stderr, one
// prefix per line of msg as git's advise() does; an empty line prints a
// bare "hint:". JSON mode emits one structured record (kind=hint). Quiet
// and porcelain modes drop it.
func Hint(msg string, args ...interface{}) {
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
