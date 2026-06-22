package logging

// NOTE: These tests mutate the global logger state (atomic.Value 'current').
// Tests must run sequentially, not in parallel. Go's default test mode handles this,
// but avoid using t.Parallel() in this package.

import (
	"io"
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

	logger := getLogger()
	if logger == nil {
		t.Fatal("getLogger() returned nil after initialization")
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
	defer closeLogger()

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
	defer closeLogger()

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
	defer closeLogger()

	dir := filepath.Dir(logFile)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("Log directory was not created")
	}

	Info("Directory creation test")

	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Fatal("Log file was not created in nested directory")
	}
}

// TestInitLogger_NoValidOutputsReturnsError pins that an empty/invalid output no
// longer silently falls back to os.Stdout (which would leak into the TUI). Such
// a configuration is a misconfiguration and must surface as an error instead of
// a silent stdout leak.
func TestInitLogger_NoValidOutputsReturnsError(t *testing.T) {
	for _, output := range []string{"", "   ", ",", " , "} {
		cfg := &Config{Level: "info", Format: "text", Output: output}
		err := InitLogger(cfg)
		if err == nil {
			t.Errorf("InitLogger(Output=%q) expected an error, got nil", output)
		}
		if err != nil && !strings.Contains(err.Error(), "no valid log outputs") {
			t.Errorf("InitLogger(Output=%q) error = %v, want it to mention 'no valid log outputs'", output, err)
		}
	}
}

// TestInitLogger_FileOnlyOutput_EmptyDefaultNoLeak proves the defense-in-depth:
// FileOnlyOutput with an empty defaultPath returns "", and InitLogger then errors
// rather than leaking to stdout. This guards the latent leak vector flagged in
// review (empty defaultPath + no file targets).
func TestInitLogger_FileOnlyOutput_EmptyDefaultNoLeak(t *testing.T) {
	stripped := FileOnlyOutput("stdout", "")
	if stripped != "" {
		t.Fatalf("FileOnlyOutput with empty defaultPath should return empty, got %q", stripped)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w

	initErr := InitLogger(&Config{Level: "info", Format: "text", Output: stripped})

	_ = w.Close()
	os.Stdout = origStdout
	outBuf, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	_ = r.Close()

	if initErr == nil {
		t.Errorf("InitLogger with empty output should have errored instead of leaking to stdout")
	}
	if len(outBuf) > 0 {
		t.Errorf("unexpected stdout output during error path: %q", string(outBuf))
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
	defer closeLogger()

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

func TestL_UninitializedReturnsDefault(t *testing.T) {
	current.Store((*loggerState)(nil))

	logger := getLogger()
	if logger == nil {
		t.Fatal("getLogger() returned nil when uninitialized")
	}
}

func Test_CloseLogger(t *testing.T) {
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

	closeLogger()

	newLogFile := filepath.Join(tmpDir, "new.log")
	cfg.Output = newLogFile
	err = InitLogger(cfg)
	if err != nil {
		t.Fatalf("InitLogger after close failed: %v", err)
	}
	defer closeLogger()

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

func Test_CloseLogger_MultipleCallsSafe(t *testing.T) {
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

	closeLogger()
	closeLogger()
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
	defer closeLogger()

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

// TestFileOnlyOutput verifies stdout/stderr are stripped for TUI mode, with a
// default applied when no file targets remain (issue: logs leaking into TUI).
func TestFileOnlyOutput(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		defaultPath string
		expected    string
	}{
		{
			name:        "dual stdout+file strips stdout",
			output:      "stdout,data/logs/javinizer.log",
			defaultPath: "data/logs/javinizer-tui.log",
			expected:    "data/logs/javinizer.log",
		},
		{
			name:        "pure stdout falls back to default",
			output:      "stdout",
			defaultPath: "data/logs/javinizer-tui.log",
			expected:    "data/logs/javinizer-tui.log",
		},
		{
			name:        "stdout+stderr falls back to default",
			output:      "stdout,stderr",
			defaultPath: "data/logs/javinizer-tui.log",
			expected:    "data/logs/javinizer-tui.log",
		},
		{
			name:        "multiple files preserved",
			output:      "stdout,/var/log/a.log,/var/log/b.log",
			defaultPath: "data/logs/javinizer-tui.log",
			expected:    "/var/log/a.log,/var/log/b.log",
		},
		{
			name:        "empty falls back to default",
			output:      "",
			defaultPath: "data/logs/javinizer-tui.log",
			expected:    "data/logs/javinizer-tui.log",
		},
		{
			name:        "file only unchanged",
			output:      "/var/log/javinizer.log",
			defaultPath: "data/logs/javinizer-tui.log",
			expected:    "/var/log/javinizer.log",
		},
		{
			name:        "whitespace trimmed",
			output:      "stdout , data/logs/x.log ",
			defaultPath: "data/logs/tui.log",
			expected:    "data/logs/x.log",
		},
		// Empty defaultPath with no file targets returns "". The safety net against
		// the resulting stdout leak now lives in InitLogger (TestInitLogger_NoValidOutputsReturnsError),
		// which errors instead of silently falling back to os.Stdout.
		{
			name:        "empty defaultPath and no file targets returns empty",
			output:      "stdout",
			defaultPath: "",
			expected:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FileOnlyOutput(tt.output, tt.defaultPath)
			if result != tt.expected {
				t.Errorf("FileOnlyOutput(%q, %q) = %q, want %q", tt.output, tt.defaultPath, result, tt.expected)
			}
		})
	}
}

// TestInitLogger_FileOnlyOutput_NoStdoutLeak proves end-to-end that when the
// output string is run through FileOnlyOutput (stripping stdout/stderr), no log
// output reaches os.Stdout. This is the guarantee the TUI relies on: even with
// the default "stdout,file" config, the TUI's terminal stays clean.
func TestInitLogger_FileOnlyOutput_NoStdoutLeak(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "noleak.log")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = origStdout }() // restore on all paths, including t.Fatalf

	stripped := FileOnlyOutput("stdout,"+logFile, "data/logs/javinizer-tui.log")
	if stripped != logFile {
		t.Fatalf("FileOnlyOutput did not strip stdout: got %q", stripped)
	}

	if err := InitLogger(&Config{Level: "info", Format: "text", Output: stripped}); err != nil {
		os.Stdout = origStdout
		_ = w.Close()
		_ = r.Close()
		t.Fatalf("InitLogger: %v", err)
	}
	defer CloseLogger()

	Info("this must not leak to stdout")

	_ = w.Close()
	os.Stdout = origStdout

	outBuf, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	_ = r.Close()
	if strings.Contains(string(outBuf), "this must not leak to stdout") {
		t.Errorf("stdout leak detected: pipe captured %q", string(outBuf))
	}

	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(content), "this must not leak to stdout") {
		t.Errorf("log file did not receive the message; content: %s", string(content))
	}
}

// TestInitLogger_StdoutNotStripped_DoesLeak proves the leak detector above is not
// vacuous: without FileOnlyOutput, a "stdout,file" output DOES write to os.Stdout.
// This is the regression the TUI fix prevents, and confirms the capture mechanism
// actually detects leaks.
func TestInitLogger_StdoutNotStripped_DoesLeak(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "leak.log")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w

	if err := InitLogger(&Config{Level: "info", Format: "text", Output: "stdout," + logFile}); err != nil {
		os.Stdout = origStdout
		_ = w.Close()
		_ = r.Close()
		t.Fatalf("InitLogger: %v", err)
	}
	defer CloseLogger()

	Info("this SHOULD leak to stdout")

	_ = w.Close()
	os.Stdout = origStdout

	outBuf, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	_ = r.Close()
	if !strings.Contains(string(outBuf), "this SHOULD leak to stdout") {
		t.Errorf("expected stdout leak but pipe captured nothing relevant: %q", string(outBuf))
	}
}
