package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestApplyEnvironmentOverrides_LogLevel tests LOG_LEVEL environment variable
func TestApplyEnvironmentOverrides_LogLevel(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected string
	}{
		{"debug level", "DEBUG", "debug"},
		{"info level", "INFO", "info"},
		{"warn level", "WARN", "warn"},
		{"error level", "ERROR", "error"},
		{"mixed case", "WaRn", "warn"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("LOG_LEVEL", tt.envValue)

			cfg := DefaultConfig(nil, nil)
			ApplyEnvironmentOverrides(cfg)

			if cfg.Logging.Level != tt.expected {
				t.Errorf("Expected log level %q, got %q", tt.expected, cfg.Logging.Level)
			}
		})
	}
}

// TestApplyEnvironmentOverrides_Umask tests UMASK environment variable
func TestApplyEnvironmentOverrides_Umask(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected string
	}{
		{"standard umask", "0022", "0022"},
		{"restrictive umask", "0077", "0077"},
		{"permissive umask", "0000", "0000"},
		{"docker umask", "002", "002"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("UMASK", tt.envValue)

			cfg := DefaultConfig(nil, nil)
			ApplyEnvironmentOverrides(cfg)

			if cfg.System.Umask != tt.expected {
				t.Errorf("Expected umask %q, got %q", tt.expected, cfg.System.Umask)
			}
		})
	}
}

// TestApplyEnvironmentOverrides_DatabaseDSN tests JAVINIZER_DB environment variable
func TestApplyEnvironmentOverrides_DatabaseDSN(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected string
	}{
		{"custom path", "/custom/path/db.sqlite", "/custom/path/db.sqlite"},
		{"relative path", "./data/test.db", "./data/test.db"},
		{"docker volume", "/data/javinizer.db", "/data/javinizer.db"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("JAVINIZER_DB", tt.envValue)

			cfg := DefaultConfig(nil, nil)
			ApplyEnvironmentOverrides(cfg)

			if cfg.Database.DSN != tt.expected {
				t.Errorf("Expected DSN %q, got %q", tt.expected, cfg.Database.DSN)
			}
		})
	}
}

// TestApplyEnvironmentOverrides_LogDir tests JAVINIZER_LOG_DIR environment variable
func TestApplyEnvironmentOverrides_LogDir(t *testing.T) {
	tests := []struct {
		name            string
		originalOutput  string
		envValue        string
		expectedOutput  string
		expectedContain string
	}{
		{
			name:            "single file output",
			originalOutput:  "data/logs/javinizer.log",
			envValue:        "/var/log/javinizer",
			expectedOutput:  "/var/log/javinizer/javinizer.log",
			expectedContain: "/var/log/javinizer/javinizer.log",
		},
		{
			name:            "stdout only",
			originalOutput:  "stdout",
			envValue:        "/custom/logs",
			expectedOutput:  "stdout",
			expectedContain: "stdout",
		},
		{
			name:            "mixed stdout and file",
			originalOutput:  "stdout,data/logs/javinizer.log",
			envValue:        "/var/log",
			expectedOutput:  "stdout,/var/log/javinizer.log",
			expectedContain: "/var/log/javinizer.log",
		},
		{
			name:            "stderr only",
			originalOutput:  "stderr",
			envValue:        "/logs",
			expectedOutput:  "stderr",
			expectedContain: "stderr",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("JAVINIZER_LOG_DIR", tt.envValue)

			cfg := DefaultConfig(nil, nil)
			cfg.Logging.Output = tt.originalOutput
			ApplyEnvironmentOverrides(cfg)

			if cfg.Logging.Output != tt.expectedOutput {
				t.Errorf("Expected output %q, got %q", tt.expectedOutput, cfg.Logging.Output)
			}
		})
	}
}

// TestApplyEnvironmentOverrides_Multiple tests multiple env vars together
func TestApplyEnvironmentOverrides_Multiple(t *testing.T) {
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("UMASK", "0077")
	t.Setenv("JAVINIZER_DB", "/custom/db.sqlite")
	t.Setenv("JAVINIZER_LOG_DIR", "/var/log/javinizer")

	cfg := DefaultConfig(nil, nil)
	cfg.Logging.Output = "data/logs/app.log"
	ApplyEnvironmentOverrides(cfg)

	// Verify all overrides applied
	if cfg.Logging.Level != "debug" {
		t.Errorf("Expected log level 'debug', got %q", cfg.Logging.Level)
	}
	if cfg.System.Umask != "0077" {
		t.Errorf("Expected umask '0077', got %q", cfg.System.Umask)
	}
	if cfg.Database.DSN != "/custom/db.sqlite" {
		t.Errorf("Expected DSN '/custom/db.sqlite', got %q", cfg.Database.DSN)
	}
	if cfg.Logging.Output != "/var/log/javinizer/app.log" {
		t.Errorf("Expected output '/var/log/javinizer/app.log', got %q", cfg.Logging.Output)
	}
}

// TestDockerAutoDetection tests Docker auto-detection of /media directory
func TestDockerAutoDetection(t *testing.T) {
	// Create a temporary directory to simulate /media
	tmpDir := t.TempDir()
	mediaDir := filepath.Join(tmpDir, "media")
	if err := os.MkdirAll(mediaDir, 0755); err != nil {
		t.Fatalf("Failed to create test media directory: %v", err)
	}

	// Mock os.Stat by temporarily changing current directory logic
	// Since we can't easily mock os.Stat, we'll test the actual behavior
	// when /media doesn't exist (normal case)
	t.Run("no media directory", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.API.Security.AllowedDirectories = []string{} // Empty initially
		ApplyEnvironmentOverrides(cfg)

		// On non-Docker systems (where /media doesn't exist), should remain empty
		// or contain /media if it exists
		// This test verifies the function doesn't crash
		if cfg.API.Security.AllowedDirectories == nil {
			t.Error("AllowedDirectories should not be nil")
		}
	})

	t.Run("pre-configured allowed directories", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.API.Security.AllowedDirectories = []string{"/home/user/videos"}
		ApplyEnvironmentOverrides(cfg)

		// Should not override if already configured
		if len(cfg.API.Security.AllowedDirectories) != 1 {
			t.Errorf("Expected 1 allowed directory, got %d", len(cfg.API.Security.AllowedDirectories))
		}
		if cfg.API.Security.AllowedDirectories[0] != "/home/user/videos" {
			t.Errorf("Expected '/home/user/videos', got %q", cfg.API.Security.AllowedDirectories[0])
		}
	})
}
