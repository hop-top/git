package output

import (
	"fmt"
	"os"

	"github.com/charmbracelet/log"
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
	// Initialize charmbracelet/log with Git-like styling
	logger = log.NewWithOptions(os.Stderr, log.Options{
		ReportCaller:    false,
		ReportTimestamp: false,
		Prefix:          "",
	})

	// Set default level to Info, but check GIT_HOP_LOG_LEVEL env var
	level := log.InfoLevel
	if envLevel := os.Getenv("GIT_HOP_LOG_LEVEL"); envLevel != "" {
		switch envLevel {
		case "debug":
			level = log.DebugLevel
		case "info":
			level = log.InfoLevel
		case "warn", "warning":
			level = log.WarnLevel
		case "error":
			level = log.ErrorLevel
		case "fatal":
			level = log.FatalLevel
		}
	}
	logger.SetLevel(level)

	// Use a minimal, Git-like formatter
	logger.SetFormatter(log.TextFormatter)
}

// SetupLogger configures the global logger based on mode and verbosity
func SetupLogger(mode Mode, verbose bool) {
	CurrentMode = mode
	Verbose = verbose

	switch mode {
	case ModeJSON:
		logger.SetFormatter(log.JSONFormatter)
		logger.SetOutput(os.Stderr)
	case ModeQuiet:
		logger.SetLevel(log.ErrorLevel)
	case ModePorcelain:
		// Porcelain mode - minimal output, no decorations
		logger.SetLevel(log.ErrorLevel)
	case ModeHuman:
		if verbose {
			logger.SetLevel(log.DebugLevel)
		} else {
			logger.SetLevel(log.InfoLevel)
		}
	}
}

// GetLogger returns the global logger instance for advanced usage
func GetLogger() *log.Logger {
	return logger
}

// Fatal prints an error and exits with status 1
func Fatal(msg string, args ...interface{}) {
	formatted := fmt.Sprintf(msg, args...)

	if CurrentMode == ModeJSON {
		logger.Error(formatted)
	} else {
		// Git-style fatal message
		fmt.Fprintf(os.Stderr, "fatal: %s\n", formatted)
	}
	os.Exit(1)
}

// Error prints a non-fatal error
func Error(msg string, args ...interface{}) {
	if CurrentMode == ModeQuiet {
		return
	}

	formatted := fmt.Sprintf(msg, args...)

	if CurrentMode == ModeJSON {
		logger.Error(formatted)
	} else {
		// Git-style error message
		fmt.Fprintf(os.Stderr, "error: %s\n", formatted)
	}
}

// Warn prints a warning message
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

// Info prints standard feedback (unless quiet/porcelain/json)
func Info(msg string, args ...interface{}) {
	if CurrentMode != ModeHuman {
		return
	}

	formatted := fmt.Sprintf(msg, args...)

	// Direct output to maintain current behavior
	// charmbracelet/log adds prefixes we don't want for normal info
	fmt.Println(formatted)
}

// Debug prints verbose logs
func Debug(msg string, args ...interface{}) {
	if !Verbose {
		return
	}

	formatted := fmt.Sprintf(msg, args...)

	if CurrentMode == ModeJSON {
		logger.Debug(formatted)
	} else {
		// Git-style debug message
		fmt.Fprintf(os.Stderr, "debug: %s\n", formatted)
	}
}

// Success prints a success message with styling
func Success(msg string, args ...interface{}) {
	if CurrentMode != ModeHuman {
		return
	}

	formatted := fmt.Sprintf(msg, args...)
	fmt.Println(formatted)
}

// WithField returns a logger with a field attached (for structured logging)
func WithField(key string, value interface{}) *log.Logger {
	return logger.With(key, value)
}

// WithFields returns a logger with multiple fields attached
func WithFields(fields map[string]interface{}) *log.Logger {
	l := logger
	for k, v := range fields {
		l = l.With(k, v)
	}
	return l
}
