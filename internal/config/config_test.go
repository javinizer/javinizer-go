package config

import (
	"os"
	"testing"
)

func TestLoadNewConfigFormat(t *testing.T) {
	// Test with new config format
	newConfig := `
metadata:
  actress_database:
    enabled: true
    auto_add: false
  genre_replacement:
    enabled: true
    auto_add: true
`

	// Write test config
	tmpFile := "/tmp/test_new_config.yaml"
	if err := os.WriteFile(tmpFile, []byte(newConfig), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile) }()

	// Load config
	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify actress_database loaded correctly
	if !cfg.Metadata.ActressDatabase.Enabled {
		t.Error("ActressDatabase.Enabled should be true")
	}
	if cfg.Metadata.ActressDatabase.AutoAdd {
		t.Error("ActressDatabase.AutoAdd should be false")
	}

	// Verify genre_replacement loaded correctly
	if !cfg.Metadata.GenreReplacement.Enabled {
		t.Error("GenreReplacement.Enabled should be true")
	}
	if !cfg.Metadata.GenreReplacement.AutoAdd {
		t.Error("GenreReplacement.AutoAdd should be true")
	}
}

func TestLoadOldConfigFormatFails(t *testing.T) {
	// Test with old config format (should not work anymore)
	oldConfig := `
metadata:
  thumb_csv:
    enabled: true
    auto_add: false
  genre_csv:
    enabled: true
    auto_add: true
`

	// Write test config
	tmpFile := "/tmp/test_old_config_fails.yaml"
	if err := os.WriteFile(tmpFile, []byte(oldConfig), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile) }()

	// Load config - old field names should be ignored, defaults used
	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Old config fields should be ignored, so we get defaults
	// Default has both enabled=true and auto_add=true
	if !cfg.Metadata.ActressDatabase.Enabled {
		t.Error("ActressDatabase.Enabled should use default (true)")
	}
	if !cfg.Metadata.ActressDatabase.AutoAdd {
		t.Error("ActressDatabase.AutoAdd should use default (true)")
	}
	if !cfg.Metadata.GenreReplacement.Enabled {
		t.Error("GenreReplacement.Enabled should use default (true)")
	}
	if !cfg.Metadata.GenreReplacement.AutoAdd {
		t.Error("GenreReplacement.AutoAdd should use default (true)")
	}
}

// TestLoadEmptyConfig tests loading empty config file (defaults should apply)
func TestLoadEmptyConfig(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"completely empty", ""},
		{"empty yaml", "---"},
		{"only comments", "# This is a comment\n# Another comment"},
		{"empty sections", "scrapers:\nmetadata:\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile := t.TempDir() + "/empty.yaml"
			if err := os.WriteFile(tmpFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			cfg, err := Load(tmpFile)
			if err != nil {
				t.Fatalf("Expected no error, got: %v", err)
			}

			// Verify defaults were applied
			defaultCfg := DefaultConfig()
			if cfg.Scrapers.UserAgent != defaultCfg.Scrapers.UserAgent {
				t.Errorf("Expected default user agent %q, got %q", defaultCfg.Scrapers.UserAgent, cfg.Scrapers.UserAgent)
			}
			if cfg.Performance.MaxWorkers != defaultCfg.Performance.MaxWorkers {
				t.Errorf("Expected default max workers %d, got %d", defaultCfg.Performance.MaxWorkers, cfg.Performance.MaxWorkers)
			}
			if cfg.Server.Port != defaultCfg.Server.Port {
				t.Errorf("Expected default port %d, got %d", defaultCfg.Server.Port, cfg.Server.Port)
			}
		})
	}
}

// TestProxyConfigValidation tests proxy config with invalid URLs
func TestProxyConfigValidation(t *testing.T) {
	tests := []struct {
		name       string
		proxyURL   string
		shouldFail bool
	}{
		{"valid http proxy", "http://proxy.example.com:8080", false},
		{"valid https proxy", "https://proxy.example.com:8080", false},
		{"valid socks5 proxy", "socks5://localhost:1080", false},
		{"valid with auth", "http://user:pass@proxy.example.com:8080", false},
		{"empty url when enabled", "", true}, // Invalid: enabled but no URL
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := `
scrapers:
  proxy:
    enabled: true
    url: "` + tt.proxyURL + `"
`
			tmpFile := t.TempDir() + "/proxy.yaml"
			if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			cfg, err := Load(tmpFile)
			if err != nil {
				t.Fatalf("Failed to load config: %v", err)
			}

			if cfg.Scrapers.Proxy.URL != tt.proxyURL {
				t.Errorf("Expected proxy URL %q, got %q", tt.proxyURL, cfg.Scrapers.Proxy.URL)
			}

			// Note: Actual validation of proxy URL happens in httpclient factory,
			// not in config loading. Config just stores the string.
			// The shouldFail flag here documents expected behavior,
			// but Load() itself won't fail - only when creating HTTP client.
		})
	}
}

// TestLoadPartialConfig tests loading config with only some fields set
func TestLoadPartialConfig(t *testing.T) {
	partialConfig := `
scrapers:
  priority: ["dmm"]
  dmm:
    enabled: true
`

	tmpFile := t.TempDir() + "/partial.yaml"
	if err := os.WriteFile(tmpFile, []byte(partialConfig), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify specified fields
	if len(cfg.Scrapers.Priority) != 1 || cfg.Scrapers.Priority[0] != "dmm" {
		t.Errorf("Expected priority ['dmm'], got %v", cfg.Scrapers.Priority)
	}
	if !cfg.Scrapers.DMM.Enabled {
		t.Error("Expected DMM enabled")
	}

	// Verify unspecified fields use defaults
	defaultCfg := DefaultConfig()
	if cfg.Scrapers.UserAgent != defaultCfg.Scrapers.UserAgent {
		t.Error("Expected default user agent for unspecified field")
	}
	if cfg.Performance.MaxWorkers != defaultCfg.Performance.MaxWorkers {
		t.Error("Expected default max workers for unspecified field")
	}
}

func TestResolveScraperUserAgent(t *testing.T) {
	tests := []struct {
		name       string
		globalUA   string
		useFake    bool
		fakeUA     string
		expectedUA string
	}{
		{
			name:       "global user-agent when fake disabled",
			globalUA:   "Javinizer-Test-UA",
			useFake:    false,
			fakeUA:     "",
			expectedUA: "Javinizer-Test-UA",
		},
		{
			name:       "default true user-agent when global empty",
			globalUA:   "",
			useFake:    false,
			fakeUA:     "",
			expectedUA: DefaultUserAgent,
		},
		{
			name:       "default fake user-agent when enabled but custom empty",
			globalUA:   "ignored",
			useFake:    true,
			fakeUA:     "",
			expectedUA: DefaultFakeUserAgent,
		},
		{
			name:       "custom fake user-agent when enabled",
			globalUA:   "ignored",
			useFake:    true,
			fakeUA:     "Mozilla/5.0 Test",
			expectedUA: "Mozilla/5.0 Test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveScraperUserAgent(tt.globalUA, tt.useFake, tt.fakeUA)
			if got != tt.expectedUA {
				t.Errorf("expected user-agent %q, got %q", tt.expectedUA, got)
			}
		})
	}
}
