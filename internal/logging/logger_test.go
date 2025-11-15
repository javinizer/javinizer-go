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
