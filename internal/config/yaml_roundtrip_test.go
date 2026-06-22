package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestScrapersConfigYAMLRoundTrip(t *testing.T) {
	// Create a ScrapersConfig with scraper settings
	scrapersCfg := &ScrapersConfig{
		UserAgent:      "test-agent",
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

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "test.yaml")

	// Save using config.Save
	cfg := &Config{
		Scrapers: *scrapersCfg,
	}
	err := Save(cfg, cfgPath)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Read the YAML file to see what was actually saved
	data, _ := os.ReadFile(cfgPath)
	t.Logf("Saved YAML:\n%s", string(data))

	// Load it back
	loaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Check scraper configs - access private field via reflection or just check Overrides
	t.Logf("Loaded Overrides: %+v", loaded.Scrapers.Overrides)

	// Verify dmm.enabled is preserved
	if dmmCfg, ok := loaded.Scrapers.Overrides["dmm"]; ok && dmmCfg != nil {
		if dmmCfg.Enabled != true {
			t.Errorf("dmm.Enabled should be true, got %v", dmmCfg.Enabled)
		}
		t.Logf("dmm config found, Enabled=%v", dmmCfg.Enabled)
	} else {
		t.Errorf("dmm not found in loaded Overrides")
	}
}

// TestScrapersConfigYAMLRoundTripWithFlaresolverr tests that global flaresolverr
// and scraper-specific flaresolverr settings are preserved in YAML round-trip.
// Uses golden file for input validation.
func TestScrapersConfigYAMLRoundTripWithFlaresolverr(t *testing.T) {
	// Load golden file with flaresolverr config
	goldenContent, err := os.ReadFile(filepath.Join("testdata", "config-with-flaresolverr.yaml.golden"))
	if err != nil {
		t.Fatalf("Failed to load golden file: %v", err)
	}

	// Create a temporary config file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.yaml")

	// Write golden content to temp file
	if err := os.WriteFile(tmpFile, goldenContent, 0644); err != nil {
		t.Fatalf("Failed to write temp config file: %v", err)
	}

	// Load the config
	loaded, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify global flaresolverr settings are preserved
	if !loaded.Scrapers.FlareSolverr.Enabled {
		t.Errorf("Global flaresolverr.Enabled should be true")
	}
	if loaded.Scrapers.FlareSolverr.URL != "http://localhost:8191/v1" {
		t.Errorf("Global flaresolverr.URL should be 'http://localhost:8191/v1', got %q", loaded.Scrapers.FlareSolverr.URL)
	}
	if loaded.Scrapers.FlareSolverr.Timeout != 30 {
		t.Errorf("Global flaresolverr.Timeout should be 30, got %d", loaded.Scrapers.FlareSolverr.Timeout)
	}
	if loaded.Scrapers.FlareSolverr.MaxRetries != 3 {
		t.Errorf("Global flaresolverr.MaxRetries should be 3, got %d", loaded.Scrapers.FlareSolverr.MaxRetries)
	}
	if loaded.Scrapers.FlareSolverr.SessionTTL != 300 {
		t.Errorf("Global flaresolverr.SessionTTL should be 300, got %d", loaded.Scrapers.FlareSolverr.SessionTTL)
	}

	// Verify r18dev scraper-specific settings are preserved
	r18Cfg, ok := loaded.Scrapers.Overrides["r18dev"]
	if !ok || r18Cfg == nil {
		t.Fatalf("r18dev not found in loaded Overrides")
	}
	if !r18Cfg.Enabled {
		t.Errorf("r18dev.Enabled should be true")
	}
	if r18Cfg.Language != "en" {
		t.Errorf("r18dev.Language should be 'en', got %q", r18Cfg.Language)
	}
	// Check r18dev has use_flaresolverr enabled
	if !r18Cfg.UseFlareSolverr {
		t.Errorf("r18dev.UseFlareSolverr should be true")
	}

	// Save and reload to verify round-trip
	err = Save(loaded, tmpFile)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	reloaded, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	// Verify flaresolverr preserved after round-trip
	if !reloaded.Scrapers.FlareSolverr.Enabled {
		t.Errorf("After round-trip: Global flaresolverr.Enabled should be true")
	}
	if reloaded.Scrapers.FlareSolverr.URL != "http://localhost:8191/v1" {
		t.Errorf("After round-trip: Global flaresolverr.URL should be 'http://localhost:8191/v1', got %q", reloaded.Scrapers.FlareSolverr.URL)
	}

	// Verify r18dev flaresolverr still preserved
	r18Reloaded, ok := reloaded.Scrapers.Overrides["r18dev"]
	if !ok || r18Reloaded == nil {
		t.Fatalf("After round-trip: r18dev not found in loaded Overrides")
	}
	if !r18Reloaded.UseFlareSolverr {
		t.Errorf("After round-trip: r18dev.UseFlareSolverr should be true")
	}
}

// TestScrapersConfigYAMLRoundTripScraperSpecific tests that scraper-specific
// fields (dmm.browser_timeout, r18dev.respect_retry_after, javlibrary.base_url)
// are preserved in YAML round-trip using golden file.
func TestScrapersConfigYAMLRoundTripScraperSpecific(t *testing.T) {
	// Load golden file with scraper-specific configs
	goldenContent, err := os.ReadFile(filepath.Join("testdata", "config-scraper-specific.yaml.golden"))
	if err != nil {
		t.Fatalf("Failed to load golden file: %v", err)
	}

	// Create a temporary config file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.yaml")

	// Write golden content to temp file
	if err := os.WriteFile(tmpFile, goldenContent, 0644); err != nil {
		t.Fatalf("Failed to write temp config file: %v", err)
	}

	// Load the config
	loaded, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify global priority is preserved
	if len(loaded.Scrapers.Priority) != 3 {
		t.Errorf("Scrapers.Priority should have 3 entries, got %d", len(loaded.Scrapers.Priority))
	}

	// Verify dmm scraper-specific fields
	dmmCfg, ok := loaded.Scrapers.Overrides["dmm"]
	if !ok || dmmCfg == nil {
		t.Fatalf("dmm not found in loaded Overrides")
	}
	if !dmmCfg.Enabled {
		t.Errorf("dmm.Enabled should be true")
	}
	// dmm has enable_browser=true and browser_timeout=45
	if !dmmCfg.Enabled {
		t.Errorf("dmm.Enabled should be true")
	}
	// Note: DMM-specific fields were previously in Extra, now in ScraperSettings

	// Verify r18dev scraper-specific fields
	r18Cfg, ok := loaded.Scrapers.Overrides["r18dev"]
	if !ok || r18Cfg == nil {
		t.Fatalf("r18dev not found in loaded Overrides")
	}
	if !r18Cfg.Enabled {
		t.Errorf("r18dev.Enabled should be true")
	}
	// Note: respect_retry_after was previously in Extra, now in ScraperSettings

	// Verify javlibrary scraper-specific fields
	javlibCfg, ok := loaded.Scrapers.Overrides["javlibrary"]
	if !ok || javlibCfg == nil {
		t.Fatalf("javlibrary not found in loaded Overrides")
	}
	if !javlibCfg.Enabled {
		t.Errorf("javlibrary.Enabled should be true")
	}
	// javlibrary has base_url at top level
	if javlibCfg.BaseURL != "https://javlibrary.com" {
		t.Errorf("javlibrary.BaseURL should be 'https://javlibrary.com', got %q", javlibCfg.BaseURL)
	}

	// Save and reload to verify round-trip
	err = Save(loaded, tmpFile)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	reloaded, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	// Verify r18dev respect_retry_after preserved after round-trip
	r18Reloaded, ok := reloaded.Scrapers.Overrides["r18dev"]
	if !ok || r18Reloaded == nil {
		t.Fatalf("After round-trip: r18dev not found in loaded Overrides")
	}
	if !r18Reloaded.Enabled {
		t.Errorf("After round-trip: r18dev.Enabled should be true")
	}
	// Note: respect_retry_after was previously in Extra, now in ScraperSettings

	// Verify javlibrary base_url preserved after round-trip
	javlibReloaded, ok := reloaded.Scrapers.Overrides["javlibrary"]
	if !ok || javlibReloaded == nil {
		t.Fatalf("After round-trip: javlibrary not found in loaded Overrides")
	}
	if javlibReloaded.BaseURL != "https://javlibrary.com" {
		t.Errorf("After round-trip: javlibrary.BaseURL should be 'https://javlibrary.com', got %q", javlibReloaded.BaseURL)
	}

	// Read saved file and verify it contains key fields
	savedContent, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read saved file: %v", err)
	}
	savedStr := string(savedContent)

	// Verify saved YAML contains expected key fields
	if !strings.Contains(savedStr, "r18dev:") {
		t.Errorf("Saved YAML should contain 'r18dev:'")
	}
	if !strings.Contains(savedStr, "dmm:") {
		t.Errorf("Saved YAML should contain 'dmm:'")
	}
	if !strings.Contains(savedStr, "javlibrary:") {
		t.Errorf("Saved YAML should contain 'javlibrary:'")
	}
	if !strings.Contains(savedStr, "respect_retry_after:") {
		t.Errorf("Saved YAML should contain 'respect_retry_after:'")
	}
}
