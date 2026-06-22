package logging_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- L(): package-level function returns current Logger ---

func TestL_ReturnsLoggerAfterInit(t *testing.T) {
	logging.InitLogger(&logging.Config{Level: "info", Format: "text", Output: "stdout"})
	l := logging.L()
	require.NotNil(t, l, "L() should return non-nil Logger after init")
}

func TestL_ReturnsLoggerWithoutInit(t *testing.T) {
	// L() should initialize with defaults if not yet initialized
	l := logging.L()
	require.NotNil(t, l, "L() should return non-nil Logger even without explicit init")
}

// --- Package-level convenience functions: Debug/Debugf/Info/Infof/Warn/Warnf/Error/Errorf ---

func TestPackageLevel_DebugAndDebugf(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "debug.log")
	logging.InitLogger(&logging.Config{Level: "debug", Format: "text", Output: logFile})
	defer logging.InitLogger(&logging.Config{Level: "info", Format: "text", Output: "stdout"}) // release file handle

	logging.Debug("pkg-debug-msg")
	logging.Debugf("pkg-debugf-%s", "formatted")

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	s := string(content)
	assert.Contains(t, s, "pkg-debug-msg")
	assert.Contains(t, s, "pkg-debugf-formatted")
}

func TestPackageLevel_InfoAndInfof(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "info.log")
	logging.InitLogger(&logging.Config{Level: "info", Format: "text", Output: logFile})
	defer logging.InitLogger(&logging.Config{Level: "info", Format: "text", Output: "stdout"}) // release file handle

	logging.Info("pkg-info-msg")
	logging.Infof("pkg-infof-%s", "formatted")

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	s := string(content)
	assert.Contains(t, s, "pkg-info-msg")
	assert.Contains(t, s, "pkg-infof-formatted")
}

func TestPackageLevel_WarnAndWarnf(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "warn.log")
	logging.InitLogger(&logging.Config{Level: "warn", Format: "text", Output: logFile})
	defer logging.InitLogger(&logging.Config{Level: "info", Format: "text", Output: "stdout"}) // release file handle

	logging.Warn("pkg-warn-msg")
	logging.Warnf("pkg-warnf-%s", "formatted")

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	s := string(content)
	assert.Contains(t, s, "pkg-warn-msg")
	assert.Contains(t, s, "pkg-warnf-formatted")
}

func TestPackageLevel_ErrorAndErrorf(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "error.log")
	logging.InitLogger(&logging.Config{Level: "error", Format: "text", Output: logFile})
	defer logging.InitLogger(&logging.Config{Level: "info", Format: "text", Output: "stdout"}) // release file handle

	logging.Error("pkg-error-msg")
	logging.Errorf("pkg-errorf-%s", "formatted")

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	s := string(content)
	assert.Contains(t, s, "pkg-error-msg")
	assert.Contains(t, s, "pkg-errorf-formatted")
}

// --- WithField / WithFields: structured logging ---

func TestWithField_LogsWithStructuredField(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "withfield.log")
	logging.InitLogger(&logging.Config{Level: "info", Format: "json", Output: logFile})
	defer logging.InitLogger(&logging.Config{Level: "info", Format: "text", Output: "stdout"}) // release file handle

	logging.WithField("job_id", 42).Info("WithField test message")

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	s := string(content)
	assert.Contains(t, s, "WithField test message")
	assert.Contains(t, s, "job_id")
}

func TestWithFields_LogsWithMultipleStructuredFields(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "withfields.log")
	logging.InitLogger(&logging.Config{Level: "info", Format: "json", Output: logFile})
	defer logging.InitLogger(&logging.Config{Level: "info", Format: "text", Output: "stdout"}) // release file handle

	logging.WithFields(logrus.Fields{
		"job_id": 42,
		"file":   "test.mp4",
	}).Info("WithFields test message")

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	s := string(content)
	assert.Contains(t, s, "WithFields test message")
	assert.Contains(t, s, "job_id")
	assert.Contains(t, s, "test.mp4")
}

// --- NoOpLogger: all methods are no-ops (test from external package) ---

func TestNoOpLogger_Debug(t *testing.T) {
	l := logging.NoOpLogger()
	l.Debug("no-op debug")
	l.Debugf("no-op debugf %s", "arg")
	// Should not panic — that's the assertion
}

func TestNoOpLogger_Info(t *testing.T) {
	l := logging.NoOpLogger()
	l.Info("no-op info")
	l.Infof("no-op infof %s", "arg")
}

func TestNoOpLogger_Warn(t *testing.T) {
	l := logging.NoOpLogger()
	l.Warn("no-op warn")
	l.Warnf("no-op warnf %s", "arg")
}

func TestNoOpLogger_Error(t *testing.T) {
	l := logging.NoOpLogger()
	l.Error("no-op error")
	l.Errorf("no-op errorf %s", "arg")
}

// --- L() returns Logger that can be used for structured logging ---

func TestL_LoggerCanLogAtAllLevels(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "all_levels.log")
	logging.InitLogger(&logging.Config{Level: "debug", Format: "text", Output: logFile})
	defer logging.InitLogger(&logging.Config{Level: "info", Format: "text", Output: "stdout"}) // release file handle

	l := logging.L()
	l.Debug("L-debug")
	l.Debugf("L-debugf %s", "x")
	l.Info("L-info")
	l.Infof("L-infof %s", "x")
	l.Warn("L-warn")
	l.Warnf("L-warnf %s", "x")
	l.Error("L-error")
	l.Errorf("L-errorf %s", "x")

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	s := string(content)
	for _, msg := range []string{"L-debug", "L-info", "L-warn", "L-error"} {
		assert.True(t, strings.Contains(s, msg), "expected log to contain %q", msg)
	}
}

// --- GlobalLogger: covers the globalLogger struct methods via the Logger interface ---

func TestGlobalLogger_CoversAllMethods(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "global_all.log")
	logging.InitLogger(&logging.Config{Level: "debug", Format: "text", Output: logFile})
	defer logging.InitLogger(&logging.Config{Level: "info", Format: "text", Output: "stdout"}) // release file handle

	l := logging.GlobalLogger()
	l.Debug("gl-debug")
	l.Debugf("gl-debugf %s", "x")
	l.Info("gl-info")
	l.Infof("gl-infof %s", "x")
	l.Warn("gl-warn")
	l.Warnf("gl-warnf %s", "x")
	l.Error("gl-error")
	l.Errorf("gl-errorf %s", "x")

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	s := string(content)
	for _, msg := range []string{"gl-debug", "gl-info", "gl-warn", "gl-error"} {
		assert.True(t, strings.Contains(s, msg), "expected log to contain %q", msg)
	}
}
