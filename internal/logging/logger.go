package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/sirupsen/logrus"
)

// loggerState holds the logger instance and associated file closers
type loggerState struct {
	logger  *logrus.Logger
	closers []io.Closer // files to close (excludes stdout/stderr)
}

// current holds the active logger state (thread-safe via atomic.Value)
var current atomic.Value // holds *loggerState

// Config represents logging configuration
type Config struct {
	Level  string `yaml:"level"`  // debug, info, warn, error
	Format string `yaml:"format"` // text, json
	Output string `yaml:"output"` // stdout, file path, or "stdout,/path/to/file.log"
}

// InitLogger initializes or reloads the global logger based on configuration.
// It atomically swaps in the new logger and closes previous file handles to prevent leaks.
// This function is safe to call multiple times for config reloading.
func InitLogger(cfg *Config) error {
	if cfg == nil {
		cfg = &Config{
			Level:  "info",
			Format: "text",
			Output: "stdout",
		}
	}

	// 1. Build new logger and open files
	logger := logrus.New()

	// Set log level
	level, err := logrus.ParseLevel(cfg.Level)
	if err != nil {
		return fmt.Errorf("invalid log level %q: %w", cfg.Level, err)
	}
	logger.SetLevel(level)

	// Set log format
	switch strings.ToLower(cfg.Format) {
	case "json":
		logger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02 15:04:05",
		})
	case "text", "":
		logger.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "2006-01-02 15:04:05",
		})
	default:
		return fmt.Errorf("invalid log format %q (must be 'text' or 'json')", cfg.Format)
	}

	// Set log output(s)
	outputs := strings.Split(cfg.Output, ",")
	var writers []io.Writer
	var closers []io.Closer // Track files to close (excludes stdout/stderr)

	for _, output := range outputs {
		output = strings.TrimSpace(output)
		if output == "" {
			continue
		}

		switch output {
		case "stdout":
			writers = append(writers, os.Stdout)
		case "stderr":
			writers = append(writers, os.Stderr)
		default:
			// It's a file path - create directory if needed
			dir := filepath.Dir(output)
			if err := os.MkdirAll(dir, 0777); err != nil {
				// Close any files we've opened so far
				for _, c := range closers {
					_ = c.Close()
				}
				return fmt.Errorf("failed to create log directory %q: %w", dir, err)
			}

			// Open file in append mode
			file, err := os.OpenFile(output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				// Close any files we've opened so far
				for _, c := range closers {
					_ = c.Close()
				}
				return fmt.Errorf("failed to open log file %q: %w", output, err)
			}

			writers = append(writers, file)
			closers = append(closers, file) // Track for cleanup
		}
	}

	// If no valid outputs, default to stdout
	if len(writers) == 0 {
		writers = append(writers, os.Stdout)
	}

	// Set output to multi-writer if multiple outputs
	if len(writers) == 1 {
		logger.SetOutput(writers[0])
	} else {
		logger.SetOutput(io.MultiWriter(writers...))
	}

	// 2. Create new state
	newState := &loggerState{
		logger:  logger,
		closers: closers,
	}

	// 3. Atomically swap in the new logger
	prevValue := current.Swap(newState)

	// 4. Close previous file handles to prevent leaks
	if prevValue != nil {
		prevState, ok := prevValue.(*loggerState)
		// Guard against typed nil pointer (can happen after CloseLogger)
		if ok && prevState != nil {
			// Close old files asynchronously to avoid blocking this call
			go func(state *loggerState) {
				for _, c := range state.closers {
					if err := c.Close(); err != nil {
						// Log to new logger (or stderr as fallback)
						_, _ = fmt.Fprintf(os.Stderr, "Warning: failed to close old log file: %v\n", err)
					}
				}
			}(prevState)
		}
	}

	return nil
}

// L returns the current logger instance. Callers should NOT cache the returned
// pointer across config reloads; instead, call logging.L() when you need to log.
// This ensures you always get the current logger after config reloads.
func L() *logrus.Logger {
	v := current.Load()
	if v == nil {
		// Fallback: initialize with defaults if not already initialized
		_ = InitLogger(nil)
		v = current.Load()
	}

	// Check if we have a valid state (handles both nil interface and typed nil pointer)
	if v == nil {
		return logrus.StandardLogger()
	}

	state, ok := v.(*loggerState)
	if !ok || state == nil {
		return logrus.StandardLogger()
	}

	return state.logger
}

// GetLogger returns the current logger instance (deprecated: use L() instead)
// This function is kept for backward compatibility but L() is preferred.
func GetLogger() *logrus.Logger {
	return L()
}

// CloseLogger closes current logger's file handles and clears the logger.
// Call during shutdown to release file descriptors. Safe to call multiple times.
func CloseLogger() {
	v := current.Load()
	if v == nil {
		return
	}
	prevValue := current.Swap((*loggerState)(nil))
	if prevValue == nil {
		return
	}
	// Guard against typed nil pointer
	prevState, ok := prevValue.(*loggerState)
	if !ok || prevState == nil {
		return
	}
	for _, c := range prevState.closers {
		if err := c.Close(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Warning: failed to close log file during shutdown: %v\n", err)
		}
	}
}

// Debug logs a debug message
func Debug(args ...interface{}) {
	GetLogger().Debug(args...)
}

// Debugf logs a formatted debug message
func Debugf(format string, args ...interface{}) {
	GetLogger().Debugf(format, args...)
}

// Info logs an info message
func Info(args ...interface{}) {
	GetLogger().Info(args...)
}

// Infof logs a formatted info message
func Infof(format string, args ...interface{}) {
	GetLogger().Infof(format, args...)
}

// Warn logs a warning message
func Warn(args ...interface{}) {
	GetLogger().Warn(args...)
}

// Warnf logs a formatted warning message
func Warnf(format string, args ...interface{}) {
	GetLogger().Warnf(format, args...)
}

// Error logs an error message
func Error(args ...interface{}) {
	GetLogger().Error(args...)
}

// Errorf logs a formatted error message
func Errorf(format string, args ...interface{}) {
	GetLogger().Errorf(format, args...)
}

// Fatal logs a fatal message and exits with status code 1.
// Note: This function calls os.Exit() and cannot be tested in unit tests.
// Any test attempting to cover this function would terminate the test process.
func Fatal(args ...interface{}) {
	GetLogger().Fatal(args...)
}

// Fatalf logs a formatted fatal message and exits with status code 1.
// Note: This function calls os.Exit() and cannot be tested in unit tests.
// Any test attempting to cover this function would terminate the test process.
func Fatalf(format string, args ...interface{}) {
	GetLogger().Fatalf(format, args...)
}

// WithField returns a logger with a single field
func WithField(key string, value interface{}) *logrus.Entry {
	return GetLogger().WithField(key, value)
}

// WithFields returns a logger with multiple fields
func WithFields(fields logrus.Fields) *logrus.Entry {
	return GetLogger().WithFields(fields)
}
