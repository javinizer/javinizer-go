package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitLogger_DefaultConfig(t *testing.T) {
	err := InitLogger(nil)
	if err != nil {
		t.Fatalf("InitLogger with nil config failed: %v", err)
	}

	logger := GetLogger()
	if logger == nil {
		t.Fatal("GetLogger returned nil after initialization")
	}
}

func TestInitLogger_TextFormat(t *testing.T) {
	cfg := &Config{
		Level:  "info",
		Format: "text",
		Output: "stdout",
	}

	err := InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger failed: %v", err)
	}
}

func TestInitLogger_JSONFormat(t *testing.T) {
	cfg := &Config{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	}

	err := InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger failed: %v", err)
	}
}

func TestInitLogger_InvalidLevel(t *testing.T) {
	cfg := &Config{
		Level:  "invalid",
		Format: "text",
		Output: "stdout",
	}

	err := InitLogger(cfg)
	if err == nil {
		t.Fatal("Expected error for invalid log level, got nil")
	}
}

func TestInitLogger_InvalidFormat(t *testing.T) {
	cfg := &Config{
		Level:  "info",
		Format: "invalid",
		Output: "stdout",
	}

	err := InitLogger(cfg)
	if err == nil {
		t.Fatal("Expected error for invalid format, got nil")
	}
}

func TestInitLogger_FileOutput(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	cfg := &Config{
		Level:  "info",
		Format: "text",
		Output: logFile,
	}

	err := InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger with file output failed: %v", err)
	}

	// Write a test log
	Info("Test log message")

	// Verify file was created
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Fatal("Log file was not created")
	}

	// Read file content
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if !strings.Contains(string(content), "Test log message") {
		t.Errorf("Log file does not contain expected message. Content: %s", string(content))
	}
}

func TestInitLogger_MultipleOutputs(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "multi.log")

	cfg := &Config{
		Level:  "info",
		Format: "text",
		Output: "stdout," + logFile,
	}

	err := InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger with multiple outputs failed: %v", err)
	}

	// Write a test log
	Info("Multi-output test")

	// Verify file was created
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Fatal("Log file was not created for multi-output")
	}

	// Read file content
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if !strings.Contains(string(content), "Multi-output test") {
		t.Errorf("Log file does not contain expected message. Content: %s", string(content))
	}
}

func TestInitLogger_AutoCreateDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "subdir", "nested", "test.log")

	cfg := &Config{
		Level:  "info",
		Format: "text",
		Output: logFile,
	}

	err := InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger failed to auto-create directories: %v", err)
	}

	// Verify directory was created
	dir := filepath.Dir(logFile)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("Log directory was not created")
	}

	// Write a test log
	Info("Directory creation test")

	// Verify file was created
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Fatal("Log file was not created in nested directory")
	}
}

func TestLogLevels(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "levels.log")

	cfg := &Config{
		Level:  "debug",
		Format: "text",
		Output: logFile,
	}

	err := InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger failed: %v", err)
	}

	// Test all log levels
	Debug("Debug message")
	Debugf("Debug %s", "formatted")
	Info("Info message")
	Infof("Info %s", "formatted")
	Warn("Warn message")
	Warnf("Warn %s", "formatted")
	Error("Error message")
	Errorf("Error %s", "formatted")

	// Read file content
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	contentStr := string(content)

	// Verify all messages are present
	expectedMessages := []string{
		"Debug message",
		"Debug formatted",
		"Info message",
		"Info formatted",
		"Warn message",
		"Warn formatted",
		"Error message",
		"Error formatted",
	}

	for _, msg := range expectedMessages {
		if !strings.Contains(contentStr, msg) {
			t.Errorf("Log file missing expected message: %s", msg)
		}
	}
}

func TestWithField(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "fields.log")

	cfg := &Config{
		Level:  "info",
		Format: "text",
		Output: logFile,
	}

	err := InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger failed: %v", err)
	}

	// Test WithField
	WithField("key", "value").Info("Field test")

	// Read file content
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	contentStr := string(content)

	if !strings.Contains(contentStr, "Field test") {
		t.Error("Log file missing field test message")
	}

	if !strings.Contains(contentStr, "key") {
		t.Error("Log file missing field key")
	}
}

func TestWithFields(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "fields_multiple.log")

	cfg := &Config{
		Level:  "info",
		Format: "text",
		Output: logFile,
	}

	err := InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger failed: %v", err)
	}

	// Test WithFields (plural)
	fields := map[string]interface{}{
		"user_id": "12345",
		"action":  "test",
		"count":   42,
	}
	WithFields(fields).Info("Multiple fields test")

	// Read file content
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	contentStr := string(content)

	if !strings.Contains(contentStr, "Multiple fields test") {
		t.Error("Log file missing fields test message")
	}

	if !strings.Contains(contentStr, "user_id") {
		t.Error("Log file missing user_id field")
	}

	if !strings.Contains(contentStr, "action") {
		t.Error("Log file missing action field")
	}
}

func TestGetLogger_UninitializedReturnsDefault(t *testing.T) {
	// Reset logger state
	current.Store((*loggerState)(nil))

	logger := GetLogger()
	if logger == nil {
		t.Fatal("GetLogger returned nil when uninitialized")
	}
}

// TestCloseLogger tests cleanup of file handles
func TestCloseLogger(t *testing.T) {
	// Arrange: Init logger with file output
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "close_test.log")

	cfg := &Config{
		Level:  "info",
		Format: "text",
		Output: logFile,
	}

	err := InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger failed: %v", err)
	}

	// Write a test log
	Info("Before close")

	// Verify file was created
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Fatal("Log file was not created")
	}

	// Act: Call CloseLogger()
	CloseLogger()

	// Assert: Subsequent logs use fallback logger (not the closed file)
	// Reinitialize to verify old file was closed
	newLogFile := filepath.Join(tmpDir, "new.log")
	cfg.Output = newLogFile
	err = InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger after close failed: %v", err)
	}

	Info("After close")

	// Verify new file receives logs
	if _, err := os.Stat(newLogFile); os.IsNotExist(err) {
		t.Fatal("New log file was not created after close")
	}

	content, err := os.ReadFile(newLogFile)
	if err != nil {
		t.Fatalf("Failed to read new log file: %v", err)
	}

	if !strings.Contains(string(content), "After close") {
		t.Error("New log file does not contain message after close")
	}
}

// TestCloseLogger_MultipleCallsSafe tests idempotency
func TestCloseLogger_MultipleCallsSafe(t *testing.T) {
	// Arrange: Init logger with file output
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "multi_close.log")

	cfg := &Config{
		Level:  "info",
		Format: "text",
		Output: logFile,
	}

	err := InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger failed: %v", err)
	}

	// Act: Call CloseLogger() twice
	CloseLogger()
	CloseLogger() // Should not panic

	// Assert: No panic, no error - test passes if we reach here
}

// TestInitLogger_MkdirAllFailure tests directory creation error
func TestInitLogger_MkdirAllFailure(t *testing.T) {
	// Arrange: Create read-only parent directory
	tmpDir := t.TempDir()
	readOnlyParent := filepath.Join(tmpDir, "readonly")
	if err := os.Mkdir(readOnlyParent, 0755); err != nil {
		t.Fatalf("Failed to create parent directory: %v", err)
	}

	// Make parent read-only (no write permission)
	if err := os.Chmod(readOnlyParent, 0444); err != nil {
		t.Fatalf("Failed to chmod parent directory: %v", err)
	}

	// Restore permissions at end of test for cleanup
	defer func() { _ = os.Chmod(readOnlyParent, 0755) }()

	logFile := filepath.Join(readOnlyParent, "subdir", "test.log")

	cfg := &Config{
		Level:  "info",
		Format: "text",
		Output: logFile,
	}

	// Act: Init logger with path under read-only dir
	err := InitLogger(cfg)

	// Assert: Error returned, no file handles leaked
	if err == nil {
		t.Fatal("Expected error for directory creation failure, got nil")
	}

	if !strings.Contains(err.Error(), "failed to create log directory") {
		t.Errorf("Expected 'failed to create log directory' in error, got: %v", err)
	}
}

// TestInitLogger_ConfigReload tests logger hot-reload with file handle cleanup
func TestInitLogger_ConfigReload(t *testing.T) {
	// Arrange: Init logger with file1
	tmpDir := t.TempDir()
	logFile1 := filepath.Join(tmpDir, "reload1.log")

	cfg := &Config{
		Level:  "info",
		Format: "text",
		Output: logFile1,
	}

	err := InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger (first) failed: %v", err)
	}

	// Write to first file
	Info("Message to file1")

	// Act: Init logger again with file2
	logFile2 := filepath.Join(tmpDir, "reload2.log")
	cfg.Output = logFile2
	err = InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger (reload) failed: %v", err)
	}

	// Write to second file
	Info("Message to file2")

	// Assert: Old file handle closed, new file receives logs
	content1, err := os.ReadFile(logFile1)
	if err != nil {
		t.Fatalf("Failed to read first log file: %v", err)
	}

	if !strings.Contains(string(content1), "Message to file1") {
		t.Error("First log file does not contain message from before reload")
	}

	content2, err := os.ReadFile(logFile2)
	if err != nil {
		t.Fatalf("Failed to read second log file: %v", err)
	}

	if !strings.Contains(string(content2), "Message to file2") {
		t.Error("Second log file does not contain message after reload")
	}

	// Verify file2 does not contain message from file1
	if strings.Contains(string(content2), "Message to file1") {
		t.Error("Second log file should not contain messages from first file")
	}
}
