package logging

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- InitLogger: rotation with MaxSizeMB > 0 ---

func TestInitLogger_Miss_RotationWithPreCreate(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "rotated.log")

	cfg := &Config{
		Level:      "info",
		Format:     "text",
		Output:     logFile,
		MaxSizeMB:  1,
		MaxBackups: 3,
		MaxAgeDays: 7,
		Compress:   true,
	}

	err := InitLogger(cfg)
	require.NoError(t, err)
	defer closeLogger()

	Info("Rotation test message")

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(content), "Rotation test message")
}

// --- InitLogger: file open failure ---

func TestInitLogger_Miss_FileOpenFailure(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a directory where we expect a file (causes open failure)
	blockingPath := filepath.Join(tmpDir, "blocked.log")
	require.NoError(t, os.Mkdir(blockingPath, 0755))

	cfg := &Config{
		Level:  "info",
		Format: "text",
		Output: blockingPath,
	}

	err := InitLogger(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open log file")
}

// --- InitLogger: file with no rotation (MaxSizeMB = 0) ---

func TestInitLogger_Miss_FileNoRotation(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "no_rotate.log")

	cfg := &Config{
		Level:     "info",
		Format:    "text",
		Output:    logFile,
		MaxSizeMB: 0, // No rotation
	}

	err := InitLogger(cfg)
	require.NoError(t, err)
	defer closeLogger()

	Info("No rotation message")

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(content), "No rotation message")
}

// --- InitLogger: multiple outputs with file ---

func TestInitLogger_Miss_MultipleOutputsWithFile(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "multi_output.log")

	cfg := &Config{
		Level:  "info",
		Format: "text",
		Output: "stdout," + logFile,
	}

	err := InitLogger(cfg)
	require.NoError(t, err)
	defer closeLogger()

	Info("Multi-output with file")

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(content), "Multi-output with file")
}

// --- InitLogger: config reload closes previous file handles ---

func TestInitLogger_Miss_ConfigReloadClosesOldHandles(t *testing.T) {
	tmpDir := t.TempDir()
	logFile1 := filepath.Join(tmpDir, "first.log")
	logFile2 := filepath.Join(tmpDir, "second.log")

	cfg := &Config{Level: "info", Format: "text", Output: logFile1}
	require.NoError(t, InitLogger(cfg))
	Info("First log")

	// Reload to different file - old file should be closed asynchronously
	cfg.Output = logFile2
	require.NoError(t, InitLogger(cfg))
	defer closeLogger()
	Info("Second log")

	// Give time for async close
	time.Sleep(100 * time.Millisecond)

	content1, err := os.ReadFile(logFile1)
	require.NoError(t, err)
	assert.Contains(t, string(content1), "First log")

	content2, err := os.ReadFile(logFile2)
	require.NoError(t, err)
	assert.Contains(t, string(content2), "Second log")
}

// --- InitLogger: nil config uses defaults ---

func TestInitLogger_Miss_NilConfigDefaults(t *testing.T) {
	require.NoError(t, InitLogger(nil))

	// Verify logger works with defaults
	Debug("Default debug")
	Info("Default info")
	Warn("Default warn")
	Error("Default error")
}

// --- InitLogger: JSON format output to file ---

func TestInitLogger_Miss_JSONFormatFileOutput(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "json.log")

	cfg := &Config{
		Level:  "debug",
		Format: "json",
		Output: logFile,
	}

	require.NoError(t, InitLogger(cfg))
	defer closeLogger()

	Info("JSON format test")

	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	// JSON format should contain the message and level
	assert.Contains(t, string(content), "JSON format test")
}

// --- getLogger: typed nil state fallback ---

func TestGetLogger_Miss_TypedNilState(t *testing.T) {
	// Store a typed nil pointer (*loggerState)(nil) - this tests the nil check guard
	current.Store((*loggerState)(nil))

	logger := getLogger()
	assert.NotNil(t, logger, "Should fallback to standard logger on typed nil state")
}

// --- closeLogger: typed nil pointer guard ---

func TestCloseLogger_Miss_TypedNilPointer(t *testing.T) {
	// Store a typed nil pointer to test the guard
	current.Store((*loggerState)(nil))

	// Should not panic
	closeLogger()
}

// --- closeLogger: normal close path ---

func TestCloseLogger_Miss_NormalClose(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "close_normal.log")

	cfg := &Config{Level: "info", Format: "text", Output: logFile}
	require.NoError(t, InitLogger(cfg))

	Info("Before close")

	// Close should succeed
	closeLogger()

	// Verify file was written before close
	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(content), "Before close")
}

// --- GetFileOutputs: edge cases ---

func TestGetFileOutputs_Miss_SingleFileOutput(t *testing.T) {
	result := GetFileOutputs("/var/log/test.log")
	assert.Equal(t, []string{"/var/log/test.log"}, result)
}

func TestGetFileOutputs_Miss_EmptyString(t *testing.T) {
	result := GetFileOutputs("")
	assert.Nil(t, result)
}

func TestGetFileOutputs_Miss_OnlyStdout(t *testing.T) {
	result := GetFileOutputs("stdout")
	assert.Nil(t, result)
}

// --- InitLogger: stderr output ---

func TestInitLogger_Miss_StderrOutput(t *testing.T) {
	cfg := &Config{
		Level:  "info",
		Format: "text",
		Output: "stderr",
	}
	require.NoError(t, InitLogger(cfg))
	Error("Stderr test")
}

// --- InitLogger: empty output returns error (no silent stdout default) ---

func TestInitLogger_Miss_EmptyOutputDefaultsToStdout(t *testing.T) {
	cfg := &Config{
		Level:  "info",
		Format: "text",
		Output: "",
	}
	err := InitLogger(cfg)
	require.Error(t, err, "empty output should return error, not silently default to stdout")
	assert.Contains(t, err.Error(), "no valid log outputs")
}

// --- InitLogger: rotation with existing file ---

func TestInitLogger_Miss_RotationWithExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "existing_rotate.log")

	// Create the file first
	require.NoError(t, os.WriteFile(logFile, []byte("existing content"), 0644))

	cfg := &Config{
		Level:      "info",
		Format:     "text",
		Output:     logFile,
		MaxSizeMB:  1,
		MaxBackups: 2,
	}
	require.NoError(t, InitLogger(cfg))
	defer closeLogger()

	Info("After rotation setup")
}
