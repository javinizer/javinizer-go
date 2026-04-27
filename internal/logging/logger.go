package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/javinizer/javinizer-go/internal/configutil"
	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
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
	Level      string `yaml:"level"`        // debug, info, warn, error
	Format     string `yaml:"format"`       // text, json
	Output     string `yaml:"output"`       // stdout, file path, or "stdout,/path/to/file.log"
	MaxSizeMB  int    `yaml:"max_size_mb"`  // Max size in MB before rotation (0 = no rotation)
	MaxBackups int    `yaml:"max_backups"`  // Max number of old log files to keep
	MaxAgeDays int    `yaml:"max_age_days"` // Max age in days to keep log files (0 = no limit)
	Compress   bool   `yaml:"compress"`     // Compress rotated files
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
			if err := os.MkdirAll(dir, configutil.DirPerm); err != nil {
				// Close any files we've opened so far
				for _, c := range closers {
					_ = c.Close()
				}
				return fmt.Errorf("failed to create log directory %q: %w", dir, err)
			}

			// Use lumberjack for rotation if MaxSizeMB > 0, otherwise plain file
			if cfg.MaxSizeMB > 0 {
				// Lumberjack defaults to 0600 permissions. Pre-create the file
				// with umask-aware permissions (configutil.FilePerm) to match non-rotation behavior.
				if _, err := os.Stat(output); os.IsNotExist(err) {
					if file, err := os.OpenFile(output, os.O_CREATE|os.O_WRONLY, configutil.FilePerm); err == nil {
						_ = file.Close()
					}
				}

				lj := &lumberjack.Logger{
					Filename:   output,
					MaxSize:    cfg.MaxSizeMB,
					MaxBackups: cfg.MaxBackups,
					MaxAge:     cfg.MaxAgeDays,
					Compress:   cfg.Compress,
				}
				writers = append(writers, lj)
				closers = append(closers, lj) // Track for cleanup
			} else {
				// No rotation - plain file append
				file, err := os.OpenFile(output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, configutil.FilePerm)
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
	L().Debug(args...)
}

// Debugf logs a formatted debug message
func Debugf(format string, args ...interface{}) {
	L().Debugf(format, args...)
}

// Info logs an info message
func Info(args ...interface{}) {
	L().Info(args...)
}

// Infof logs a formatted info message
func Infof(format string, args ...interface{}) {
	L().Infof(format, args...)
}

// Warn logs a warning message
func Warn(args ...interface{}) {
	L().Warn(args...)
}

// Warnf logs a formatted warning message
func Warnf(format string, args ...interface{}) {
	L().Warnf(format, args...)
}

// Error logs an error message
func Error(args ...interface{}) {
	L().Error(args...)
}

// Errorf logs a formatted error message
func Errorf(format string, args ...interface{}) {
	L().Errorf(format, args...)
}

// Fatal logs a fatal message and exits with status code 1.
// Note: This function calls os.Exit() and cannot be tested in unit tests.
// Any test attempting to cover this function would terminate the test process.
func Fatal(args ...interface{}) {
	L().Fatal(args...)
}

// Fatalf logs a formatted fatal message and exits with status code 1.
// Note: This function calls os.Exit() and cannot be tested in unit tests.
// Any test attempting to cover this function would terminate the test process.
func Fatalf(format string, args ...interface{}) {
	L().Fatalf(format, args...)
}

// WithField returns a logger with a single field
func WithField(key string, value interface{}) *logrus.Entry {
	return L().WithField(key, value)
}

// WithFields returns a logger with multiple fields
func WithFields(fields logrus.Fields) *logrus.Entry {
	return L().WithFields(fields)
}

// GetFileOutputs extracts file paths from a comma-separated output string.
// Returns only file paths (excludes "stdout" and "stderr").
// Returns nil if no file outputs are found.
func GetFileOutputs(output string) []string {
	outputs := strings.Split(output, ",")
	var files []string
	for _, o := range outputs {
		o = strings.TrimSpace(o)
		if o != "" && o != "stdout" && o != "stderr" {
			files = append(files, o)
		}
	}
	if len(files) == 0 {
		return nil
	}
	return files
}
