package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

// loggerState holds the logger instance and associated file closers
type loggerState struct {
	logger  *logrus.Logger
	closers []io.Closer // files to close (excludes stdout/stderr)
}

// globalInit is the package-level LoggerInitializer singleton.
// Production code uses InitLogger / L via this singleton;
// test code constructs its own loggerInit for isolation.
var globalInit = newLoggerInit()

// current is an alias to globalInit.current for backward compatibility
// with test code that resets logger state directly.
//
//nolint:unused // used by same-package tests
var current = &globalInit.current

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

// LoggerInitializer is the seam for initializing and accessing the logger.
// Production code uses the package-level singleton; test code constructs its
// own instance with a backing atomic.Value for isolation.
type LoggerInitializer interface {
	// Init initializes or reloads the logger based on configuration.
	// It atomically swaps in the new logger and closes previous file handles.
	Init(cfg *Config) error
	// L returns the current Logger instance.
	L() Logger
}

// loggerInit holds the atomic logger state and implements LoggerInitializer.
type loggerInit struct {
	current atomic.Value // holds *loggerState
}

// newLoggerInit creates a LoggerInitializer backed by a fresh atomic.Value.
func newLoggerInit() *loggerInit {
	li := &loggerInit{}
	return li
}

// resolveLogWriters resolves the output configuration into io.Writer and io.Closer
// slices. It isolates the lumberjack/plain-file branching and directory creation
// from the Init orchestrator, making the writer resolution independently testable.
//
// The closers slice contains writers that must be closed on logger reload
// (excludes os.Stdout and os.Stderr). On error, any opened files are closed
// before returning.
func resolveLogWriters(cfg *Config) ([]io.Writer, []io.Closer, error) {
	outputs := strings.Split(cfg.Output, ",")
	var writers []io.Writer
	var closers []io.Closer

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
			if err := os.MkdirAll(dir, config.DirPerm); err != nil {
				// Close any files we've opened so far
				for _, c := range closers {
					_ = c.Close()
				}
				return nil, nil, fmt.Errorf("failed to create log directory %q: %w", dir, err)
			}

			// Use lumberjack for rotation if MaxSizeMB > 0, otherwise plain file
			if cfg.MaxSizeMB > 0 {
				// Lumberjack defaults to 0600 permissions. Pre-create the file
				// with umask-aware permissions (config.FilePerm) to match non-rotation behavior.
				if info, err := os.Stat(output); err != nil {
					if !os.IsNotExist(err) {
						// Stat failed for a reason other than "not exists" (e.g. permission);
						// surface it instead of letting logger setup silently succeed with
						// an unusable output file.
						for _, c := range closers {
							_ = c.Close()
						}
						return nil, nil, fmt.Errorf("failed to stat log file %q: %w", output, err)
					}
					// File does not exist — create it with the intended permissions.
					file, err := os.OpenFile(output, os.O_CREATE|os.O_WRONLY, config.FilePerm)
					if err != nil {
						for _, c := range closers {
							_ = c.Close()
						}
						return nil, nil, fmt.Errorf("failed to create log file %q: %w", output, err)
					}
					_ = file.Close()
					_ = info
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
				file, err := os.OpenFile(output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, config.FilePerm)
				if err != nil {
					// Close any files we've opened so far
					for _, c := range closers {
						_ = c.Close()
					}
					return nil, nil, fmt.Errorf("failed to open log file %q: %w", output, err)
				}
				writers = append(writers, file)
				closers = append(closers, file) // Track for cleanup
			}
		}
	}

	// If no valid outputs were configured, return an error rather than silently
	// falling back to os.Stdout. A silent stdout fallback can corrupt TUI/
	// AltScreen displays and hides misconfigurations (e.g. an empty output string
	// produced by FileOnlyOutput with an empty defaultPath).
	if len(writers) == 0 {
		for _, c := range closers {
			_ = c.Close()
		}
		return nil, nil, fmt.Errorf("no valid log outputs configured in %q (expected stdout, stderr, or a file path)", cfg.Output)
	}

	return writers, closers, nil
}

// buildLogrusLogger constructs a logrus.Logger from config, setting level, format,
// and output writers. This isolates logger construction from the Init orchestrator.
func buildLogrusLogger(cfg *Config, writers []io.Writer) (*logrus.Logger, error) {
	logger := logrus.New()

	// Set log level
	level, err := logrus.ParseLevel(cfg.Level)
	if err != nil {
		return nil, fmt.Errorf("invalid log level %q: %w", cfg.Level, err)
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
		return nil, fmt.Errorf("invalid log format %q (must be 'text' or 'json')", cfg.Format)
	}

	// Set output to multi-writer if multiple outputs
	if len(writers) == 1 {
		logger.SetOutput(writers[0])
	} else {
		logger.SetOutput(io.MultiWriter(writers...))
	}

	return logger, nil
}

// Init initializes or reloads the logger based on configuration.
// It atomically swaps in the new logger and closes previous file handles to prevent leaks.
// This function is safe to call multiple times for config reloading.
//
// The three-step orchestrator: resolve writers → build logger → swap + cleanup.
func (li *loggerInit) Init(cfg *Config) error {
	if cfg == nil {
		cfg = &Config{
			Level:  "info",
			Format: "text",
			Output: "stdout",
		}
	}

	// Step 1: Resolve output writers from config.
	writers, closers, err := resolveLogWriters(cfg)
	if err != nil {
		return err
	}

	// Step 2: Build the logrus logger with level, format, and output.
	logger, err := buildLogrusLogger(cfg, writers)
	if err != nil {
		// Clean up any opened files since we won't be using them.
		for _, c := range closers {
			_ = c.Close()
		}
		return err
	}

	// Step 3: Create new state, atomically swap, and clean up previous.
	newState := &loggerState{
		logger:  logger,
		closers: closers,
	}

	prevValue := li.current.Swap(newState)

	// Close previous file handles to prevent leaks
	if prevValue != nil {
		prevState, ok := prevValue.(*loggerState)
		// Guard against typed nil pointer (can happen after closeLogger)
		if ok && prevState != nil {
			// Close old files asynchronously to avoid blocking this call
			go func(state *loggerState) {
				defer func() {
					if r := recover(); r != nil {
						_, _ = fmt.Fprintf(os.Stderr, "Logger async closer panicked: %v\n", r)
					}
				}()
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

// InitLogger initializes or reloads the global logger based on configuration.
// It delegates to the package-level singleton LoggerInitializer.
// This function is safe to call multiple times for config reloading.
func InitLogger(cfg *Config) error {
	return globalInit.Init(cfg)
}

// CloseLogger synchronously closes the current logger's file handles and resets
// the global logger state to nil. Used by tests that need to ensure all file
// handles are released before t.TempDir() cleanup, and by the TUI on shutdown.
func CloseLogger() {
	prevValue := globalInit.current.Swap((*loggerState)(nil))
	if prevValue == nil {
		return
	}
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

// L returns the current Logger instance from the package-level singleton.
func L() Logger {
	return globalInit.L()
}

// getLogger returns the current logrus.Logger instance, initializing with defaults if needed.
func getLogger() *logrus.Logger {
	v := globalInit.current.Load()
	if v == nil {
		_ = globalInit.Init(nil)
		v = globalInit.current.Load()
	}

	if v == nil {
		return logrus.StandardLogger()
	}

	state, ok := v.(*loggerState)
	if !ok || state == nil {
		return logrus.StandardLogger()
	}

	return state.logger
}

// L returns the current Logger instance, initializing with defaults if needed.
func (li *loggerInit) L() Logger {
	// Return an adapter that resolves the active logger on every call so that
	// callers which store logging.L() keep writing to the current logger after
	// InitLogger reloads (rather than holding onto a stale *logrus.Logger).
	return &logrusLoggerAdapter{init: li}
}

// getLogrus returns the current logrus.Logger, initializing with defaults if needed.
func (li *loggerInit) getLogrus() *logrus.Logger {
	v := li.current.Load()
	if v == nil {
		_ = li.Init(nil)
		v = li.current.Load()
	}

	if v == nil {
		return logrus.StandardLogger()
	}

	state, ok := v.(*loggerState)
	if !ok || state == nil {
		return logrus.StandardLogger()
	}

	return state.logger
}

// logrusLoggerAdapter adapts a *logrus.Logger to the Logger interface. It holds
// a reference to the initializer (not a logger pointer) so it stays reload-aware:
// each call resolves the current logger, picking up InitLogger reinitialization.
type logrusLoggerAdapter struct {
	init *loggerInit
}

func (a *logrusLoggerAdapter) logger() *logrus.Logger { return a.init.getLogrus() }

func (a *logrusLoggerAdapter) Debug(args ...any)                 { a.logger().Debug(args...) }
func (a *logrusLoggerAdapter) Debugf(format string, args ...any) { a.logger().Debugf(format, args...) }
func (a *logrusLoggerAdapter) Info(args ...any)                  { a.logger().Info(args...) }
func (a *logrusLoggerAdapter) Infof(format string, args ...any)  { a.logger().Infof(format, args...) }
func (a *logrusLoggerAdapter) Warn(args ...any)                  { a.logger().Warn(args...) }
func (a *logrusLoggerAdapter) Warnf(format string, args ...any)  { a.logger().Warnf(format, args...) }
func (a *logrusLoggerAdapter) Error(args ...any)                 { a.logger().Error(args...) }
func (a *logrusLoggerAdapter) Errorf(format string, args ...any) { a.logger().Errorf(format, args...) }

// closeLogger closes current logger's file handles and clears the logger.
// Call during shutdown to release file descriptors. Safe to call multiple times.
//
//nolint:unused // used by same-package tests
func closeLogger() {
	v := globalInit.current.Load()
	if v == nil {
		return
	}
	prevValue := globalInit.current.Swap((*loggerState)(nil))
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
func Debug(args ...any) {
	getLogger().Debug(args...)
}

// Debugf logs a formatted debug message
func Debugf(format string, args ...any) {
	getLogger().Debugf(format, args...)
}

// Info logs an info message
func Info(args ...any) {
	getLogger().Info(args...)
}

// Infof logs a formatted info message
func Infof(format string, args ...any) {
	getLogger().Infof(format, args...)
}

// Warn logs a warning message
func Warn(args ...any) {
	getLogger().Warn(args...)
}

// Warnf logs a formatted warning message
func Warnf(format string, args ...any) {
	getLogger().Warnf(format, args...)
}

// Error logs an error message
func Error(args ...any) {
	getLogger().Error(args...)
}

// Errorf logs a formatted error message
func Errorf(format string, args ...any) {
	getLogger().Errorf(format, args...)
}

// WithField returns a logrus.Entry with a single structured field.
// Use for machine-parseable logging: WithField("job_id", id).Info("Job started")
func WithField(key string, value any) *logrus.Entry {
	return getLogger().WithField(key, value)
}

// WithFields returns a logrus.Entry with multiple structured fields.
// Use for machine-parseable logging: WithFields(logrus.Fields{"job_id": id, "file": path}).Info("Processing")
func WithFields(fields logrus.Fields) *logrus.Entry {
	return getLogger().WithFields(fields)
}

// FileOnlyOutput returns the comma-separated file targets from output, stripping
// stdout/stderr. If no file targets remain, defaultPath is returned. Used by
// the TUI to ensure logs go to file only (not the terminal) so the TUI display
// isn't corrupted.
func FileOnlyOutput(output, defaultPath string) string {
	files := GetFileOutputs(output)
	if len(files) == 0 {
		return defaultPath
	}
	return strings.Join(files, ",")
}

// Logger is the seam interface for structured logging. Consumers that need
// testable logging should depend on this interface rather than calling the
// package-level logging.Debug/Info/Warn/Error functions directly.
//
// The production implementation (GlobalLogger) delegates to the package-level
// functions. Tests inject mock or spy implementations via DI.
//
// Migration strategy: add the Logger field to DI containers (BatchJobDeps,
// workflowFactoryConfig, CoreDeps/APIDeps) and consume it in new code.
// Existing package-level calls remain valid — migrate incrementally, not
// all 483 calls at once.
type Logger interface {
	// Debug logs a debug-level message.
	Debug(args ...any)
	// Debugf logs a formatted debug-level message.
	Debugf(format string, args ...any)
	// Info logs an info-level message.
	Info(args ...any)
	// Infof logs a formatted info-level message.
	Infof(format string, args ...any)
	// Warn logs a warning-level message.
	Warn(args ...any)
	// Warnf logs a formatted warning-level message.
	Warnf(format string, args ...any)
	// Error logs an error-level message.
	Error(args ...any)
	// Errorf logs a formatted error-level message.
	Errorf(format string, args ...any)
}

// globalLogger delegates to the package-level logging functions.
// It is the default Logger implementation used by production DI containers.
type globalLogger struct{}

// GlobalLogger returns a Logger that delegates to the package-level
// logging functions (Debug, Info, Warn, Error, etc.). This is the
// default implementation for production DI containers.
func GlobalLogger() Logger {
	return &globalLogger{}
}

func (g *globalLogger) Debug(args ...any) { Debug(args...) }
func (g *globalLogger) Debugf(format string, args ...any) {
	Debugf(format, args...)
}
func (g *globalLogger) Info(args ...any) { Info(args...) }
func (g *globalLogger) Infof(format string, args ...any) {
	Infof(format, args...)
}
func (g *globalLogger) Warn(args ...any) { Warn(args...) }
func (g *globalLogger) Warnf(format string, args ...any) {
	Warnf(format, args...)
}
func (g *globalLogger) Error(args ...any) { Error(args...) }
func (g *globalLogger) Errorf(format string, args ...any) {
	Errorf(format, args...)
}

// NoOpLogger returns a Logger that discards all output.
// Useful in tests that don't assert on log output.
func NoOpLogger() Logger {
	return &noOpLogger{}
}

type noOpLogger struct{}

func (n *noOpLogger) Debug(_ ...any)            {}
func (n *noOpLogger) Debugf(_ string, _ ...any) {}
func (n *noOpLogger) Info(_ ...any)             {}
func (n *noOpLogger) Infof(_ string, _ ...any)  {}
func (n *noOpLogger) Warn(_ ...any)             {}
func (n *noOpLogger) Warnf(_ string, _ ...any)  {}
func (n *noOpLogger) Error(_ ...any)            {}
func (n *noOpLogger) Errorf(_ string, _ ...any) {}

// SetOutput swaps the global logger's output writer and returns a restoration
// function. Intended for tests that need to assert on log output. Call the
// returned function (typically deferred) to restore the previous writer.
func SetOutput(w io.Writer) func() {
	l := getLogger()
	prev := l.Out
	l.SetOutput(w)
	return func() { l.SetOutput(prev) }
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
