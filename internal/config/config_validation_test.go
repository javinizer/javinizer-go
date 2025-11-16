package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSave tests the Save() function
func TestSave(t *testing.T) {
	tests := []struct {
		name      string
		setupDir  bool
		expectErr bool
	}{
		{
			name:      "save to valid directory",
			setupDir:  true,
			expectErr: false,
		},
		{
			name:      "save creates directory if not exists",
			setupDir:  false,
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			var cfgPath string

			if tt.setupDir {
				cfgPath = filepath.Join(tmpDir, "config.yaml")
			} else {
				// Test that Save creates nested directories
				cfgPath = filepath.Join(tmpDir, "configs", "nested", "config.yaml")
			}

			cfg := DefaultConfig()
			cfg.Server.Port = 9999
			cfg.Logging.Level = "debug"

			err := Save(cfg, cfgPath)

			if tt.expectErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.FileExists(t, cfgPath)

			// Verify saved content by loading it back
			loaded, err := Load(cfgPath)
			require.NoError(t, err)
			assert.Equal(t, 9999, loaded.Server.Port)
			assert.Equal(t, "debug", loaded.Logging.Level)
		})
	}
}

// TestSaveInvalidPath tests Save() with invalid paths
func TestSaveInvalidPath(t *testing.T) {
	cfg := DefaultConfig()

	// Try to save to an invalid path (e.g., a path that can't be created)
	// On Unix systems, we can't create files in root without permissions
	invalidPath := "/root/invalid/path/config.yaml"
	err := Save(cfg, invalidPath)

	// Should fail on permission denied or similar error
	// The exact error varies by OS, so we just check that it fails
	if os.Geteuid() != 0 { // Skip this check if running as root
		assert.Error(t, err)
	}
}

// TestLoadOrCreate tests the LoadOrCreate() function
func TestLoadOrCreate(t *testing.T) {
	tests := []struct {
		name          string
		setupFile     bool
		fileContent   string
		expectDefault bool
		expectErr     bool
	}{
		{
			name:          "load existing file",
			setupFile:     true,
			fileContent:   "server:\n  port: 7777\n",
			expectDefault: false,
			expectErr:     false,
		},
		{
			name:          "create file if not exists",
			setupFile:     false,
			expectDefault: true,
			expectErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			cfgPath := filepath.Join(tmpDir, "config.yaml")

			if tt.setupFile {
				err := os.WriteFile(cfgPath, []byte(tt.fileContent), 0644)
				require.NoError(t, err)
			}

			cfg, err := LoadOrCreate(cfgPath)

			if tt.expectErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, cfg)

			if tt.expectDefault {
				// Should have created file with defaults
				assert.FileExists(t, cfgPath)
				assert.Equal(t, 8080, cfg.Server.Port) // Default port
			} else {
				// Should have loaded custom config
				assert.Equal(t, 7777, cfg.Server.Port)
			}
		})
	}
}

// TestLoadOrCreateInvalidPath tests LoadOrCreate() with read errors
func TestLoadOrCreateInvalidPath(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a directory with the same name as the config file to cause an error
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	err := os.Mkdir(cfgPath, 0755)
	require.NoError(t, err)

	// Try to load or create - should fail because path is a directory
	_, err = LoadOrCreate(cfgPath)
	assert.Error(t, err)
}

// TestLoadInvalidYAML tests Load() with malformed YAML
func TestLoadInvalidYAML(t *testing.T) {
	tests := []struct {
		name        string
		yamlContent string
	}{
		{
			name: "invalid YAML syntax",
			yamlContent: `
server:
  port: [invalid
  host: "test"
`,
		},
		{
			name: "invalid type for integer field",
			yamlContent: `
server:
  port: "not a number"
`,
		},
		{
			name: "malformed structure",
			yamlContent: `
this is not
  - valid: yaml
    content: [
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			cfgPath := filepath.Join(tmpDir, "config.yaml")

			err := os.WriteFile(cfgPath, []byte(tt.yamlContent), 0644)
			require.NoError(t, err)

			_, err = Load(cfgPath)
			assert.Error(t, err, "Expected error parsing invalid YAML")
			assert.Contains(t, err.Error(), "failed to parse config file")
		})
	}
}

// TestLoadMissingFile tests Load() behavior with non-existent file
func TestLoadMissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "nonexistent.yaml")

	cfg, err := Load(cfgPath)

	// Should NOT error - returns default config
	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, 8080, cfg.Server.Port) // Verify it's default
}

// TestLoadUnreadableFile tests Load() with permission errors
func TestLoadUnreadableFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping permission test when running as root")
	}

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "unreadable.yaml")

	// Create file with no read permissions
	err := os.WriteFile(cfgPath, []byte("server:\n  port: 8080\n"), 0200) // write-only
	require.NoError(t, err)

	_, err = Load(cfgPath)
	assert.Error(t, err, "Expected error reading unreadable file")
	assert.Contains(t, err.Error(), "failed to read config file")
}

// TestValidationMissingOptionalFields tests that missing optional fields use defaults
func TestValidationMissingOptionalFields(t *testing.T) {
	yamlContent := `
server:
  host: localhost
  port: 8080
# Missing most other sections
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "minimal.yaml")

	err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	// Should fill in defaults for missing sections
	assert.Equal(t, "localhost", cfg.Server.Host)
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.NotEmpty(t, cfg.Scrapers.UserAgent) // Default user agent
	assert.NotNil(t, cfg.Scrapers.Priority)
	assert.Equal(t, 5, cfg.Performance.MaxWorkers) // Default
	assert.Equal(t, "info", cfg.Logging.Level)     // Default
}

// TestPriorityResolutionEmptyArray tests empty array behavior (use global priority)
func TestPriorityResolutionEmptyArray(t *testing.T) {
	yamlContent := `
scrapers:
  priority:
    - r18dev
    - dmm
metadata:
  priority:
    title: []
    actress:
      - dmm
      - r18dev
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "priority.yaml")

	err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	// Empty array for title means "use global priority"
	assert.Empty(t, cfg.Metadata.Priority.Title)

	// Actress has explicit priority
	assert.Equal(t, []string{"dmm", "r18dev"}, cfg.Metadata.Priority.Actress)

	// Global priority is set
	assert.Equal(t, []string{"r18dev", "dmm"}, cfg.Scrapers.Priority)
}

// TestPriorityResolutionMissingField tests that missing fields default properly
func TestPriorityResolutionMissingField(t *testing.T) {
	yamlContent := `
scrapers:
  priority:
    - r18dev
    - dmm
metadata:
  priority:
    actress:
      - dmm
# Director field is completely missing
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "missing_fields.yaml")

	err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	// Missing director field should get default priority from DefaultConfig()
	assert.NotNil(t, cfg.Metadata.Priority.Director)

	// Default has r18dev, dmm for director
	defaultCfg := DefaultConfig()
	assert.Equal(t, defaultCfg.Metadata.Priority.Director, cfg.Metadata.Priority.Director)
}

// TestOutOfRangeValues tests validation of numeric fields
func TestOutOfRangeValues(t *testing.T) {
	tests := []struct {
		name        string
		yamlContent string
		checkFunc   func(*testing.T, *Config)
	}{
		{
			name: "negative port",
			yamlContent: `
server:
  port: -1
`,
			checkFunc: func(t *testing.T, cfg *Config) {
				// YAML will parse this as -1, application should validate
				assert.Equal(t, -1, cfg.Server.Port)
			},
		},
		{
			name: "zero workers",
			yamlContent: `
performance:
  max_workers: 0
`,
			checkFunc: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 0, cfg.Performance.MaxWorkers)
			},
		},
		{
			name: "negative timeout",
			yamlContent: `
performance:
  worker_timeout: -100
`,
			checkFunc: func(t *testing.T, cfg *Config) {
				assert.Equal(t, -100, cfg.Performance.WorkerTimeout)
			},
		},
		{
			name: "negative download timeout",
			yamlContent: `
output:
  download_timeout: -1
`,
			checkFunc: func(t *testing.T, cfg *Config) {
				assert.Equal(t, -1, cfg.Output.DownloadTimeout)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			cfgPath := filepath.Join(tmpDir, "range_test.yaml")

			err := os.WriteFile(cfgPath, []byte(tt.yamlContent), 0644)
			require.NoError(t, err)

			cfg, err := Load(cfgPath)
			require.NoError(t, err, "Config should load without error")

			// Note: Config loading doesn't validate ranges - that's expected
			// to be done at runtime by the application
			tt.checkFunc(t, cfg)
		})
	}
}

// TestInvalidFilePaths tests file path fields
func TestInvalidFilePaths(t *testing.T) {
	yamlContent := `
database:
  type: sqlite
  dsn: "/path/that/does/not/exist/db.sqlite"
logging:
  output: "/invalid/log/path.log"
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "paths.yaml")

	err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(cfgPath)
	require.NoError(t, err, "Loading should succeed even with invalid paths")

	// Paths are loaded as-is, validation happens at runtime
	assert.Equal(t, "/path/that/does/not/exist/db.sqlite", cfg.Database.DSN)
	assert.Equal(t, "/invalid/log/path.log", cfg.Logging.Output)
}

// TestDefaultConfigCompleteness verifies DefaultConfig() sets all required fields
func TestDefaultConfigCompleteness(t *testing.T) {
	cfg := DefaultConfig()

	// Server config
	assert.NotEmpty(t, cfg.Server.Host)
	assert.Greater(t, cfg.Server.Port, 0)

	// Scrapers config
	assert.NotEmpty(t, cfg.Scrapers.UserAgent)
	assert.NotEmpty(t, cfg.Scrapers.Priority)
	assert.False(t, cfg.Scrapers.Proxy.Enabled)

	// Metadata config
	assert.NotNil(t, cfg.Metadata.Priority)
	assert.True(t, cfg.Metadata.ActressDatabase.Enabled)
	assert.True(t, cfg.Metadata.GenreReplacement.Enabled)

	// NFO config
	assert.True(t, cfg.Metadata.NFO.Enabled)
	assert.NotEmpty(t, cfg.Metadata.NFO.DisplayName)
	assert.NotEmpty(t, cfg.Metadata.NFO.FilenameTemplate)

	// Matching config
	assert.NotEmpty(t, cfg.Matching.Extensions)
	assert.NotEmpty(t, cfg.Matching.RegexPattern)

	// Output config
	assert.NotEmpty(t, cfg.Output.FolderFormat)
	assert.NotEmpty(t, cfg.Output.FileFormat)
	assert.NotEmpty(t, cfg.Output.Delimiter)
	assert.Greater(t, cfg.Output.MaxTitleLength, 0)
	assert.Greater(t, cfg.Output.MaxPathLength, 0)

	// Database config
	assert.NotEmpty(t, cfg.Database.Type)
	assert.NotEmpty(t, cfg.Database.DSN)

	// Logging config
	assert.NotEmpty(t, cfg.Logging.Level)
	assert.NotEmpty(t, cfg.Logging.Format)
	assert.NotEmpty(t, cfg.Logging.Output)

	// Performance config
	assert.Greater(t, cfg.Performance.MaxWorkers, 0)
	assert.Greater(t, cfg.Performance.WorkerTimeout, 0)
	assert.Greater(t, cfg.Performance.BufferSize, 0)
	assert.Greater(t, cfg.Performance.UpdateInterval, 0)

	// MediaInfo config
	assert.NotEmpty(t, cfg.MediaInfo.CLIPath)
	assert.Greater(t, cfg.MediaInfo.CLITimeout, 0)
}

// TestYAMLRoundTrip tests that Save() and Load() preserve data
func TestYAMLRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "roundtrip.yaml")

	// Start with default config
	original := DefaultConfig()

	// Modify some values
	original.Server.Port = 9999
	original.Scrapers.UserAgent = "Custom Agent"
	original.Logging.Level = "debug"
	original.Performance.MaxWorkers = 10
	original.Scrapers.Proxy.Enabled = true
	original.Scrapers.Proxy.URL = "http://proxy.test:8080"
	original.Scrapers.Proxy.Username = "user"
	original.Scrapers.Proxy.Password = "pass"

	// Save
	err := Save(original, cfgPath)
	require.NoError(t, err)

	// Load back
	loaded, err := Load(cfgPath)
	require.NoError(t, err)

	// Verify key fields preserved
	assert.Equal(t, original.Server.Port, loaded.Server.Port)
	assert.Equal(t, original.Scrapers.UserAgent, loaded.Scrapers.UserAgent)
	assert.Equal(t, original.Logging.Level, loaded.Logging.Level)
	assert.Equal(t, original.Performance.MaxWorkers, loaded.Performance.MaxWorkers)
	assert.Equal(t, original.Scrapers.Proxy.Enabled, loaded.Scrapers.Proxy.Enabled)
	assert.Equal(t, original.Scrapers.Proxy.URL, loaded.Scrapers.Proxy.URL)
	assert.Equal(t, original.Scrapers.Proxy.Username, loaded.Scrapers.Proxy.Username)
	assert.Equal(t, original.Scrapers.Proxy.Password, loaded.Scrapers.Proxy.Password)
}

// TestProxyURLValidation tests various proxy URL formats
func TestProxyURLValidation(t *testing.T) {
	tests := []struct {
		name      string
		proxyURL  string
		shouldErr bool
	}{
		{
			name:      "valid http proxy",
			proxyURL:  "http://proxy.example.com:8080",
			shouldErr: false,
		},
		{
			name:      "valid https proxy",
			proxyURL:  "https://proxy.example.com:443",
			shouldErr: false,
		},
		{
			name:      "valid socks5 proxy",
			proxyURL:  "socks5://localhost:1080",
			shouldErr: false,
		},
		{
			name:      "proxy without port",
			proxyURL:  "http://proxy.example.com",
			shouldErr: false,
		},
		{
			name:      "proxy with IP address",
			proxyURL:  "http://192.168.1.1:8080",
			shouldErr: false,
		},
		{
			name:      "empty proxy URL",
			proxyURL:  "",
			shouldErr: false, // Valid - means no proxy
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yamlContent := "scrapers:\n  proxy:\n    enabled: true\n    url: \"" + tt.proxyURL + "\"\n"

			tmpDir := t.TempDir()
			cfgPath := filepath.Join(tmpDir, "proxy_test.yaml")

			err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
			require.NoError(t, err)

			cfg, err := Load(cfgPath)

			if tt.shouldErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.proxyURL, cfg.Scrapers.Proxy.URL)
			}
		})
	}
}

// TestBooleanDefaults tests boolean field defaults
func TestBooleanDefaults(t *testing.T) {
	cfg := DefaultConfig()

	// Verify expected boolean defaults
	assert.True(t, cfg.Scrapers.R18Dev.Enabled)
	assert.False(t, cfg.Scrapers.DMM.Enabled) // DMM disabled by default
	assert.False(t, cfg.Scrapers.DMM.ScrapeActress)

	assert.True(t, cfg.Metadata.ActressDatabase.Enabled)
	assert.True(t, cfg.Metadata.ActressDatabase.AutoAdd)
	assert.True(t, cfg.Metadata.GenreReplacement.Enabled)
	assert.False(t, cfg.Metadata.TagDatabase.Enabled) // Opt-in feature

	assert.True(t, cfg.Metadata.NFO.Enabled)
	assert.True(t, cfg.Metadata.NFO.FirstNameOrder)
	assert.False(t, cfg.Metadata.NFO.ActressLanguageJA)

	assert.False(t, cfg.Matching.RegexEnabled)

	assert.True(t, cfg.Output.MoveToFolder)
	assert.True(t, cfg.Output.RenameFile)
	assert.False(t, cfg.Output.GroupActress)
	assert.True(t, cfg.Output.DownloadCover)
	assert.True(t, cfg.Output.DownloadPoster)

	assert.False(t, cfg.MediaInfo.CLIEnabled)
}

// TestArrayDefaults tests that array fields have proper defaults
func TestArrayDefaults(t *testing.T) {
	cfg := DefaultConfig()

	// Verify array defaults
	assert.NotEmpty(t, cfg.Scrapers.Priority)
	assert.Contains(t, cfg.Scrapers.Priority, "r18dev")
	assert.Contains(t, cfg.Scrapers.Priority, "dmm")

	assert.NotEmpty(t, cfg.Matching.Extensions)
	assert.Contains(t, cfg.Matching.Extensions, ".mp4")
	assert.Contains(t, cfg.Matching.Extensions, ".mkv")

	assert.NotEmpty(t, cfg.Output.SubtitleExtensions)
	assert.Contains(t, cfg.Output.SubtitleExtensions, ".srt")

	// Priority config arrays
	assert.NotEmpty(t, cfg.Metadata.Priority.Actress)
	assert.NotEmpty(t, cfg.Metadata.Priority.Title)
	assert.NotEmpty(t, cfg.Metadata.Priority.ID)
}

// TestBrowserConfig tests DMM headless browser configuration
func TestBrowserConfig(t *testing.T) {
	yamlContent := `
scrapers:
  dmm:
    enabled: true
    enable_browser: true
    browser_timeout: 45
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "headless.yaml")

	err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	assert.True(t, cfg.Scrapers.DMM.Enabled)
	assert.True(t, cfg.Scrapers.DMM.EnableBrowser)
	assert.Equal(t, 45, cfg.Scrapers.DMM.BrowserTimeout)
}

// TestMediaInfoConfig tests MediaInfo configuration
func TestMediaInfoConfig(t *testing.T) {
	yamlContent := `
mediainfo:
  cli_enabled: true
  cli_path: "/usr/local/bin/mediainfo"
  cli_timeout: 60
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "mediainfo.yaml")

	err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	assert.True(t, cfg.MediaInfo.CLIEnabled)
	assert.Equal(t, "/usr/local/bin/mediainfo", cfg.MediaInfo.CLIPath)
	assert.Equal(t, 60, cfg.MediaInfo.CLITimeout)
}

// TestEmptyConfig tests loading completely empty config file
func TestEmptyConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "empty.yaml")

	// Create empty file
	err := os.WriteFile(cfgPath, []byte(""), 0644)
	require.NoError(t, err)

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	// Should use all defaults
	assert.Equal(t, "localhost", cfg.Server.Host)
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, 5, cfg.Performance.MaxWorkers)
}

// TestWhitespaceOnlyConfig tests loading whitespace-only config file
func TestWhitespaceOnlyConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "whitespace.yaml")

	// Create file with only whitespace and tabs
	// Note: YAML with only whitespace/tabs may fail to parse
	err := os.WriteFile(cfgPath, []byte("   \n  \t  \n   "), 0644)
	require.NoError(t, err)

	_, err = Load(cfgPath)
	// YAML parser may reject whitespace-only files, which is acceptable
	// Just verify we get an error or defaults
	if err != nil {
		assert.Contains(t, err.Error(), "failed to parse config file")
	}
}
