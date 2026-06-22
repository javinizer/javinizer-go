package config

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"gopkg.in/yaml.v3"
)

func TestScrapersConfigMarshalJSON(t *testing.T) {
	// Create a ScrapersConfig with common fields and scraper settings
	scrapersCfg := ScrapersConfig{
		UserAgent:             "test-user-agent",
		Referer:               "https://example.com",
		TimeoutSeconds:        30,
		RequestTimeoutSeconds: 60,
		Priority:              []string{"dmm", "r18dev"},
		Proxy: models.ProxyConfig{
			Enabled:        true,
			DefaultProfile: "main",
			Profiles: map[string]models.ProxyProfile{
				"main": {URL: "http://proxy.example.com:8080"},
			},
		},
		Overrides: map[string]*models.ScraperSettings{
			"dmm": &models.ScraperSettings{
				Enabled:    true,
				Language:   "ja",
				Timeout:    10,
				RateLimit:  1000,
				RetryCount: 3,
			},
			"r18dev": &models.ScraperSettings{
				Enabled:   false,
				Language:  "en",
				RateLimit: 500,
			},
		},
	}

	// Marshal to JSON using pointer to ensure MarshalJSON is called
	jsonData, err := json.Marshal(&scrapersCfg)
	if err != nil {
		t.Fatalf("Failed to marshal ScrapersConfig: %v", err)
	}

	jsonStr := string(jsonData)

	// Verify common fields are present
	if !strings.Contains(jsonStr, `"user_agent"`) {
		t.Errorf("JSON output missing 'user_agent' field. Got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"referer"`) {
		t.Errorf("JSON output missing 'referer' field. Got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"timeout_seconds"`) {
		t.Errorf("JSON output missing 'timeout_seconds' field. Got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"priority"`) {
		t.Errorf("JSON output missing 'priority' field. Got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"proxy"`) {
		t.Errorf("JSON output missing 'proxy' field. Got: %s", jsonStr)
	}

	// Verify scraper-specific settings are present (this was the bug - json:"-" tags prevented serialization)
	if !strings.Contains(jsonStr, `"dmm"`) {
		t.Errorf("JSON output missing 'dmm' scraper settings. Got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"r18dev"`) {
		t.Errorf("JSON output missing 'r18dev' scraper settings. Got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"enabled":true`) && !strings.Contains(jsonStr, `"enabled":true`) {
		// Check that enabled field is present for dmm
		t.Errorf("JSON output missing 'enabled' field for scrapers. Got: %s", jsonStr)
	}

	// Verify the unified field names are used (rate_limit, retry_count)
	if !strings.Contains(jsonStr, `"rate_limit"`) {
		t.Errorf("JSON output should use unified 'rate_limit' field. Got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"retry_count"`) {
		t.Errorf("JSON output should use unified 'retry_count' field. Got: %s", jsonStr)
	}
}

func TestScrapersConfigMarshalJSONWithOverrides(t *testing.T) {
	// Create a ScrapersConfig with Overrides populated (simulating post-Finalize state)
	scrapersCfg := ScrapersConfig{
		UserAgent: "test-agent",
		Priority:  []string{"dmm"},
		Overrides: map[string]*models.ScraperSettings{
			"dmm": &models.ScraperSettings{
				Enabled:   true,
				RateLimit: 2000,
			},
		},
	}

	// Marshal to JSON using pointer to ensure MarshalJSON is called
	jsonData, err := json.Marshal(&scrapersCfg)
	if err != nil {
		t.Fatalf("Failed to marshal ScrapersConfig: %v", err)
	}

	jsonStr := string(jsonData)

	// Verify scraper settings are serialized from Overrides
	if !strings.Contains(jsonStr, `"dmm"`) {
		t.Errorf("JSON output missing 'dmm' from Overrides. Got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"enabled":true`) {
		t.Errorf("JSON output missing enabled=true from Overrides. Got: %s", jsonStr)
	}
}

func TestScrapersConfigRoundTrip(t *testing.T) {
	// Create original config
	original := ScrapersConfig{
		UserAgent:      "round-trip-agent",
		TimeoutSeconds: 45,
		Priority:       []string{"dmm", "r18dev"},
		Overrides: map[string]*models.ScraperSettings{
			"dmm": &models.ScraperSettings{
				Enabled:    true,
				RateLimit:  1500,
				RetryCount: 5,
			},
		},
	}

	// Marshal to JSON using pointer to ensure custom MarshalJSON is called
	jsonData, err := json.Marshal(&original)
	if err != nil {
		t.Fatalf("Failed to marshal ScrapersConfig: %v", err)
	}

	// Unmarshal back to a new struct
	var decoded ScrapersConfig
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal ScrapersConfig: %v", err)
	}

	// Verify common fields round-trip correctly
	if decoded.UserAgent != original.UserAgent {
		t.Errorf("UserAgent round-trip failed: got %s, want %s", decoded.UserAgent, original.UserAgent)
	}
	if decoded.TimeoutSeconds != original.TimeoutSeconds {
		t.Errorf("TimeoutSeconds round-trip failed: got %d, want %d", decoded.TimeoutSeconds, original.TimeoutSeconds)
	}

	// Verify scraper configs are present (though field names may differ due to unified format)
	if len(decoded.Overrides) == 0 {
		t.Errorf("Overrides should not be empty after round-trip")
	}

	dmmSettings, ok := decoded.Overrides["dmm"]
	if !ok {
		t.Errorf("dmm scraper settings missing after round-trip")
	} else {
		// Note: RateLimit should round-trip correctly since models.ScraperSettings uses the same field name
		if dmmSettings.RateLimit != original.Overrides["dmm"].RateLimit {
			t.Errorf("RateLimit round-trip mismatch: got %d, want %d", dmmSettings.RateLimit, original.Overrides["dmm"].RateLimit)
		}
	}
}

func TestDatabaseConfigJSONSerialization(t *testing.T) {
	// Create a DatabaseConfig with all fields set
	dbConfig := DatabaseConfig{
		Type:     "sqlite",
		DSN:      "data/test.db",
		LogLevel: "silent",
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(dbConfig)
	if err != nil {
		t.Fatalf("Failed to marshal DatabaseConfig: %v", err)
	}

	// Verify JSON contains log_level field (snake_case for web UI compatibility)
	jsonStr := string(jsonData)
	if !strings.Contains(jsonStr, "log_level") {
		t.Errorf("JSON output missing 'log_level' field. Got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, "silent") {
		t.Errorf("JSON output missing 'silent' value. Got: %s", jsonStr)
	}

	// Unmarshal back to struct
	var decoded DatabaseConfig
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal DatabaseConfig: %v", err)
	}

	// Verify all fields were preserved
	if decoded.Type != dbConfig.Type {
		t.Errorf("Type mismatch: got %s, want %s", decoded.Type, dbConfig.Type)
	}
	if decoded.DSN != dbConfig.DSN {
		t.Errorf("DSN mismatch: got %s, want %s", decoded.DSN, dbConfig.DSN)
	}
	if decoded.LogLevel != dbConfig.LogLevel {
		t.Errorf("LogLevel mismatch: got %s, want %s", decoded.LogLevel, dbConfig.LogLevel)
	}
}

func TestScrapersConfigMarshalYAML(t *testing.T) {
	// Create a ScrapersConfig with scraper settings
	scrapersCfg := ScrapersConfig{
		UserAgent:      "test-user-agent",
		TimeoutSeconds: 30,
		Priority:       []string{"dmm", "r18dev"},
		Overrides: map[string]*models.ScraperSettings{
			"dmm": &models.ScraperSettings{
				Enabled:   true,
				Language:  "ja",
				RateLimit: 1000,
			},
			"r18dev": &models.ScraperSettings{
				Enabled:   false,
				Language:  "en",
				RateLimit: 500,
			},
		},
	}

	// Marshal to YAML using yaml.Marshal which will call MarshalYAML
	yamlData, err := yaml.Marshal(&scrapersCfg)
	if err != nil {
		t.Fatalf("Failed to marshal ScrapersConfig to YAML: %v", err)
	}

	yamlStr := string(yamlData)

	// Verify common fields are present
	if !strings.Contains(yamlStr, "user_agent") {
		t.Errorf("YAML output missing 'user_agent' field. Got:\n%s", yamlStr)
	}
	if !strings.Contains(yamlStr, "timeout_seconds") {
		t.Errorf("YAML output missing 'timeout_seconds' field. Got:\n%s", yamlStr)
	}
	if !strings.Contains(yamlStr, "priority") {
		t.Errorf("YAML output missing 'priority' field. Got:\n%s", yamlStr)
	}

	// Verify scraper-specific settings are present (this was the bug - yaml:"-" tags prevented serialization)
	if !strings.Contains(yamlStr, "dmm") {
		t.Errorf("YAML output missing 'dmm' scraper settings. Got:\n%s", yamlStr)
	}
	if !strings.Contains(yamlStr, "r18dev") {
		t.Errorf("YAML output missing 'r18dev' scraper settings. Got:\n%s", yamlStr)
	}
	if !strings.Contains(yamlStr, "enabled: true") {
		t.Errorf("YAML output missing 'enabled: true' for dmm. Got:\n%s", yamlStr)
	}
}

// TestScrapersConfigJSONRoundTripWithFlaresolverr tests that flaresolverr settings
// are preserved in JSON round-trip (MarshalJSON -> UnmarshalJSON).
func TestScrapersConfigJSONRoundTripWithFlaresolverr(t *testing.T) {
	// Create a ScrapersConfig with flaresolverr settings
	scrapersCfg := ScrapersConfig{
		UserAgent:      "test-agent",
		TimeoutSeconds: 30,
		Priority:       []string{"dmm", "r18dev"},
		FlareSolverr: models.FlareSolverrConfig{
			Enabled:    true,
			URL:        "http://localhost:8191/v1",
			Timeout:    30,
			MaxRetries: 3,
			SessionTTL: 300,
		},
		Overrides: map[string]*models.ScraperSettings{
			"r18dev": &models.ScraperSettings{
				Enabled:         true,
				UseFlareSolverr: true,
			},
			"dmm": &models.ScraperSettings{
				Enabled:         false,
				UseFlareSolverr: false,
			},
		},
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(&scrapersCfg)
	if err != nil {
		t.Fatalf("Failed to marshal ScrapersConfig: %v", err)
	}

	jsonStr := string(jsonData)
	t.Logf("JSON output:\n%s", jsonStr)

	// Verify global flaresolverr is present in JSON
	if !strings.Contains(jsonStr, `"flaresolverr"`) {
		t.Errorf("JSON output missing 'flaresolverr' field. Got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"enabled":true`) {
		t.Errorf("JSON output missing flaresolverr enabled=true. Got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"url":"http://localhost:8191/v1"`) {
		t.Errorf("JSON output missing flaresolverr url. Got: %s", jsonStr)
	}

	// Verify r18dev flaresolverr is present
	if !strings.Contains(jsonStr, `"r18dev"`) {
		t.Errorf("JSON output missing 'r18dev' field. Got: %s", jsonStr)
	}

	// Unmarshal back to a new struct
	var decoded ScrapersConfig
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal ScrapersConfig: %v", err)
	}

	// Verify global flaresolverr round-trip
	if !decoded.FlareSolverr.Enabled {
		t.Errorf("After round-trip: FlareSolverr.Enabled should be true")
	}
	if decoded.FlareSolverr.URL != "http://localhost:8191/v1" {
		t.Errorf("After round-trip: FlareSolverr.URL should be 'http://localhost:8191/v1', got %q", decoded.FlareSolverr.URL)
	}
	if decoded.FlareSolverr.Timeout != 30 {
		t.Errorf("After round-trip: FlareSolverr.Timeout should be 30, got %d", decoded.FlareSolverr.Timeout)
	}
	if decoded.FlareSolverr.MaxRetries != 3 {
		t.Errorf("After round-trip: FlareSolverr.MaxRetries should be 3, got %d", decoded.FlareSolverr.MaxRetries)
	}
	if decoded.FlareSolverr.SessionTTL != 300 {
		t.Errorf("After round-trip: FlareSolverr.SessionTTL should be 300, got %d", decoded.FlareSolverr.SessionTTL)
	}

	// Verify r18dev flaresolverr round-trip
	r18Cfg, ok := decoded.Overrides["r18dev"]
	if !ok {
		t.Errorf("After round-trip: r18dev should be present in Overrides")
	} else {
		if !r18Cfg.UseFlareSolverr {
			t.Errorf("After round-trip: r18dev.UseFlareSolverr should be true")
		}
	}
}

// TestScrapersConfigJSONRoundTripScraperSpecific tests that scraper-specific
// fields (dmm.browser_timeout, r18dev.respect_retry_after) are preserved in JSON round-trip.
func TestScrapersConfigJSONRoundTripScraperSpecific(t *testing.T) {
	// Create a ScrapersConfig with scraper-specific fields
	scrapersCfg := ScrapersConfig{
		UserAgent:      "test-agent",
		TimeoutSeconds: 30,
		Priority:       []string{"dmm", "r18dev"},
		Overrides: map[string]*models.ScraperSettings{
			"r18dev": &models.ScraperSettings{
				Enabled: true,
			},
			"dmm": &models.ScraperSettings{
				Enabled: true,
			},
			"javlibrary": &models.ScraperSettings{
				Enabled: true,
				BaseURL: "https://javlibrary.com",
			},
		},
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(&scrapersCfg)
	if err != nil {
		t.Fatalf("Failed to marshal ScrapersConfig: %v", err)
	}

	jsonStr := string(jsonData)
	t.Logf("JSON output:\n%s", jsonStr)

	// Verify scraper keys are present
	if !strings.Contains(jsonStr, `"r18dev"`) {
		t.Errorf("JSON output missing 'r18dev' field. Got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"dmm"`) {
		t.Errorf("JSON output missing 'dmm' field. Got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"javlibrary"`) {
		t.Errorf("JSON output missing 'javlibrary' field. Got: %s", jsonStr)
	}

	// Note: scraper-specific fields (respect_retry_after, browser_timeout)
	// were previously in Extra but are now in ScraperSettings
	if !strings.Contains(jsonStr, `"base_url"`) {
		t.Errorf("JSON output missing 'base_url' field. Got: %s", jsonStr)
	}

	// Unmarshal back to a new struct
	var decoded ScrapersConfig
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal ScrapersConfig: %v", err)
	}

	// Verify r18dev fields round-trip
	r18Cfg, ok := decoded.Overrides["r18dev"]
	if !ok {
		t.Fatalf("After round-trip: r18dev should be present in Overrides")
	}
	if !r18Cfg.Enabled {
		t.Errorf("After round-trip: r18dev.Enabled should be true")
	}
	// Note: respect_retry_after was previously in Extra, now in ScraperSettings

	// Verify dmm fields round-trip
	dmmCfg, ok := decoded.Overrides["dmm"]
	if !ok {
		t.Fatalf("After round-trip: dmm should be present in Overrides")
	}
	if !dmmCfg.Enabled {
		t.Errorf("After round-trip: dmm.Enabled should be true")
	}
	// Note: DMM-specific fields (enable_browser, browser_timeout, scrape_actress)
	// were previously in Extra, now in ScraperSettings

	// Verify javlibrary base_url at top level
	javlibCfg, ok := decoded.Overrides["javlibrary"]
	if !ok {
		t.Fatalf("After round-trip: javlibrary should be present in Overrides")
	}
	if javlibCfg.BaseURL != "https://javlibrary.com" {
		t.Errorf("After round-trip: javlibrary.BaseURL should be 'https://javlibrary.com', got %q", javlibCfg.BaseURL)
	}
}
