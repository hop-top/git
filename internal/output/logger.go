package output

import (
	"fmt"
	"os"

	"charm.land/log/v2"
	"github.com/spf13/viper"
	kitlog "hop.top/kit/log"
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

	logger = kitlog.WithLevel(rootViper(), level)

	if mode == ModeJSON {
		logger.SetFormatter(log.JSONFormatter)
	}
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
	formatted := fmt.Sprintf(msg, args...)
	if CurrentMode == ModeJSON {
		logger.Error(formatted)
	} else {
		fmt.Fprintf(os.Stderr, "fatal: %s\n", formatted)
	}
	os.Exit(1)
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

// Warn prints a warning message.
func Warn(msg string, args ...interface{}) {
	if CurrentMode == ModeQuiet {
		return
	}
	formatted := fmt.Sprintf(msg, args...)
	if CurrentMode == ModeJSON {
		logger.Warn(formatted)
	} else if CurrentMode == ModeHuman {
		logger.Warn(formatted)
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
