package logging

// NOTE: These tests mutate the global logger state (atomic.Value 'current').
// Tests must run sequentially, not in parallel. Go's default test mode handles this,
// but avoid using t.Parallel() in this package.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInitLogger_DefaultConfig(t *testing.T) {
	err := InitLogger(nil)
	if err != nil {
		t.Fatalf("InitLogger with nil config failed: %v", err)
	}

	logger := L()
	if logger == nil {
		t.Fatal("L() returned nil after initialization")
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
	defer CloseLogger()

	Info("Test log message")

	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Fatal("Log file was not created")
	}

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
	defer CloseLogger()

	Info("Multi-output test")

	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Fatal("Log file was not created for multi-output")
	}

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
	defer CloseLogger()

	dir := filepath.Dir(logFile)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("Log directory was not created")
	}

	Info("Directory creation test")

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
	defer CloseLogger()

	Debug("Debug message")
	Debugf("Debug %s", "formatted")
	Info("Info message")
	Infof("Info %s", "formatted")
	Warn("Warn message")
	Warnf("Warn %s", "formatted")
	Error("Error message")
	Errorf("Error %s", "formatted")

	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	contentStr := string(content)

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
	defer CloseLogger()

	WithField("key", "value").Info("Field test")

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
	defer CloseLogger()

	fields := map[string]interface{}{
		"user_id": "12345",
		"action":  "test",
		"count":   42,
	}
	WithFields(fields).Info("Multiple fields test")

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

func TestL_UninitializedReturnsDefault(t *testing.T) {
	current.Store((*loggerState)(nil))

	logger := L()
	if logger == nil {
		t.Fatal("L() returned nil when uninitialized")
	}
}

func TestCloseLogger(t *testing.T) {
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

	Info("Before close")

	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Fatal("Log file was not created")
	}

	CloseLogger()

	newLogFile := filepath.Join(tmpDir, "new.log")
	cfg.Output = newLogFile
	err = InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger after close failed: %v", err)
	}

	Info("After close")

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

func TestCloseLogger_MultipleCallsSafe(t *testing.T) {
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

	CloseLogger()
	CloseLogger()
}

func TestInitLogger_MkdirAllFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not enforce Unix-style directory permissions")
	}

	tmpDir := t.TempDir()
	readOnlyParent := filepath.Join(tmpDir, "readonly")
	if err := os.Mkdir(readOnlyParent, 0755); err != nil {
		t.Fatalf("Failed to create parent directory: %v", err)
	}

	if err := os.Chmod(readOnlyParent, 0444); err != nil {
		t.Fatalf("Failed to chmod parent directory: %v", err)
	}

	defer func() { _ = os.Chmod(readOnlyParent, 0755) }()

	logFile := filepath.Join(readOnlyParent, "subdir", "test.log")

	cfg := &Config{
		Level:  "info",
		Format: "text",
		Output: logFile,
	}

	err := InitLogger(cfg)

	if err == nil {
		t.Fatal("Expected error for directory creation failure, got nil")
	}

	if !strings.Contains(err.Error(), "failed to create log directory") {
		t.Errorf("Expected 'failed to create log directory' in error, got: %v", err)
	}
}

func TestInitLogger_ConfigReload(t *testing.T) {
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

	Info("Message to file1")

	logFile2 := filepath.Join(tmpDir, "reload2.log")
	cfg.Output = logFile2
	err = InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger (reload) failed: %v", err)
	}

	Info("Message to file2")

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

	if strings.Contains(string(content2), "Message to file1") {
		t.Error("Second log file should not contain messages from first file")
	}
}

func TestGetFileOutputs(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected []string
	}{
		{
			name:     "stdout only",
			output:   "stdout",
			expected: nil,
		},
		{
			name:     "stderr only",
			output:   "stderr",
			expected: nil,
		},
		{
			name:     "file only",
			output:   "/var/log/javinizer.log",
			expected: []string{"/var/log/javinizer.log"},
		},
		{
			name:     "stdout and file",
			output:   "stdout,/var/log/javinizer.log",
			expected: []string{"/var/log/javinizer.log"},
		},
		{
			name:     "multiple files",
			output:   "stdout,/var/log/a.log,/var/log/b.log",
			expected: []string{"/var/log/a.log", "/var/log/b.log"},
		},
		{
			name:     "spaces trimmed",
			output:   "stdout , /var/log/test.log ",
			expected: []string{"/var/log/test.log"},
		},
		{
			name:     "empty string",
			output:   "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetFileOutputs(tt.output)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("Expected nil, got %v", result)
				}
				return
			}
			if result == nil {
				t.Errorf("Expected %v, got nil", tt.expected)
				return
			}
			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d files, got %d: %v", len(tt.expected), len(result), result)
				return
			}
			for i, exp := range tt.expected {
				if result[i] != exp {
					t.Errorf("Expected file[%d] = %q, got %q", i, exp, result[i])
				}
			}
		})
	}
}
