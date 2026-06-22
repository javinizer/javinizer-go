package logging

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Logger interface: GlobalLogger delegates to package-level functions ---

func TestGlobalLogger_DelegatesDebug(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := tmpDir + "/debug.log"
	require.NoError(t, InitLogger(&Config{Level: "debug", Format: "text", Output: logFile}))
	defer closeLogger()

	l := GlobalLogger()
	l.Debug("global-debug-msg")
	l.Debugf("global-debugf-%s", "formatted")

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	s := string(content)
	assert.Contains(t, s, "global-debug-msg")
	assert.Contains(t, s, "global-debugf-formatted")
}

func TestGlobalLogger_DelegatesInfo(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := tmpDir + "/info.log"
	require.NoError(t, InitLogger(&Config{Level: "info", Format: "text", Output: logFile}))
	defer closeLogger()

	l := GlobalLogger()
	l.Info("global-info-msg")
	l.Infof("global-infof-%s", "formatted")

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	s := string(content)
	assert.Contains(t, s, "global-info-msg")
	assert.Contains(t, s, "global-infof-formatted")
}

func TestGlobalLogger_DelegatesWarn(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := tmpDir + "/warn.log"
	require.NoError(t, InitLogger(&Config{Level: "warn", Format: "text", Output: logFile}))
	defer closeLogger()

	l := GlobalLogger()
	l.Warn("global-warn-msg")
	l.Warnf("global-warnf-%s", "formatted")

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	s := string(content)
	assert.Contains(t, s, "global-warn-msg")
	assert.Contains(t, s, "global-warnf-formatted")
}

func TestGlobalLogger_DelegatesError(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := tmpDir + "/error.log"
	require.NoError(t, InitLogger(&Config{Level: "error", Format: "text", Output: logFile}))
	defer closeLogger()

	l := GlobalLogger()
	l.Error("global-error-msg")
	l.Errorf("global-errorf-%s", "formatted")

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	s := string(content)
	assert.Contains(t, s, "global-error-msg")
	assert.Contains(t, s, "global-errorf-formatted")
}

// --- NoOpLogger: all methods are no-ops ---

func TestNoOpLogger_DiscardsAll(t *testing.T) {
	l := NoOpLogger()
	// None of these should panic or produce output
	l.Debug("noop-debug")
	l.Debugf("noop-debugf %s", "arg")
	l.Info("noop-info")
	l.Infof("noop-infof %s", "arg")
	l.Warn("noop-warn")
	l.Warnf("noop-warnf %s", "arg")
	l.Error("noop-error")
	l.Errorf("noop-errorf %s", "arg")
}

// --- Logger interface compliance (compile-time checks) ---

func TestGlobalLogger_ImplementsLogger(t *testing.T) {
	var _ Logger = GlobalLogger()
}

func TestNoOpLogger_ImplementsLogger(t *testing.T) {
	var _ Logger = NoOpLogger()
}

// --- SpyLogger: test helper for asserting on log calls ---

func TestSpyLogger_CapturesMessages(t *testing.T) {
	spy := NewSpyLogger()
	spy.Info("hello")
	spy.Warnf("warning %d", 42)

	assert.Equal(t, []string{"hello"}, spy.Messages("info"))
	assert.Equal(t, []string{"warning 42"}, spy.Messages("warnf"))
	assert.Empty(t, spy.Messages("debug"))
}

// Compile-time check for SpyLogger
func TestSpyLogger_ImplementsLogger(t *testing.T) {
	var _ Logger = NewSpyLogger()
}

// ---------------------------------------------------------------------------
// SpyLogger — test spy that records log calls for assertion
// ---------------------------------------------------------------------------

// SpyLogger is a test spy that records log calls for assertion.
// It implements the Logger interface. Use Messages(level) to retrieve
// recorded messages by level key (e.g. "info", "warnf", "debug").
type SpyLogger struct {
	messages map[string][]string
}

// NewSpyLogger creates a SpyLogger that records all log calls.
func NewSpyLogger() *SpyLogger {
	return &SpyLogger{
		messages: make(map[string][]string),
	}
}

func (s *SpyLogger) Debug(args ...any) {
	s.record("debug", args...)
}

func (s *SpyLogger) Debugf(format string, args ...any) {
	s.recordf("debugf", format, args...)
}

func (s *SpyLogger) Info(args ...any) {
	s.record("info", args...)
}

func (s *SpyLogger) Infof(format string, args ...any) {
	s.recordf("infof", format, args...)
}

func (s *SpyLogger) Warn(args ...any) {
	s.record("warn", args...)
}

func (s *SpyLogger) Warnf(format string, args ...any) {
	s.recordf("warnf", format, args...)
}

func (s *SpyLogger) Error(args ...any) {
	s.record("error", args...)
}

func (s *SpyLogger) Errorf(format string, args ...any) {
	s.recordf("errorf", format, args...)
}

// Messages returns recorded messages for a given level key (e.g. "info", "warnf").
func (s *SpyLogger) Messages(level string) []string {
	return s.messages[level]
}

func (s *SpyLogger) record(level string, args ...any) {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = fmt.Sprint(a)
	}
	s.messages[level] = append(s.messages[level], strings.TrimSpace(strings.Join(parts, " ")))
}

func (s *SpyLogger) recordf(level, format string, args ...any) {
	s.messages[level] = append(s.messages[level], strings.TrimSpace(fmt.Sprintf(format, args...)))
}
