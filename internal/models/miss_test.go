package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- HistoryOperation Scan: nil value ---

func TestHistoryOperation_Scan_NilValue(t *testing.T) {
	var op HistoryOperation
	err := op.Scan(nil)
	require.NoError(t, err)
	assert.Equal(t, HistoryOperation(""), op)
}

// --- HistoryStatus Scan: nil value ---

func TestHistoryStatus_Scan_NilValue_Miss(t *testing.T) {
	var s HistoryStatus
	err := s.Scan(nil)
	require.NoError(t, err)
	assert.Equal(t, HistoryStatus(""), s)
}

// --- HistoryOperation MarshalJSON/UnmarshalJSON ---

func TestHistoryOperation_MarshalUnmarshalJSON(t *testing.T) {
	op := HistoryOpScrape
	data, err := json.Marshal(op)
	require.NoError(t, err)
	assert.Equal(t, `"scrape"`, string(data))

	var decoded HistoryOperation
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, HistoryOpScrape, decoded)
}

func TestHistoryOperation_UnmarshalJSON_Invalid(t *testing.T) {
	var op HistoryOperation
	err := json.Unmarshal([]byte(`123`), &op)
	assert.Error(t, err)
}

// --- HistoryStatus MarshalJSON/UnmarshalJSON ---

func TestHistoryStatus_MarshalUnmarshalJSON(t *testing.T) {
	s := HistoryStatusSuccess
	data, err := json.Marshal(s)
	require.NoError(t, err)
	assert.Equal(t, `"success"`, string(data))

	var decoded HistoryStatus
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, HistoryStatusSuccess, decoded)
}

func TestHistoryStatus_UnmarshalJSON_Invalid(t *testing.T) {
	var s HistoryStatus
	err := json.Unmarshal([]byte(`123`), &s)
	assert.Error(t, err)
}

// --- ScraperSettings MarshalJSON at 75% ---

func TestScraperSettings_MarshalJSON_Full(t *testing.T) {
	s := &ScraperSettings{
		Enabled:                true,
		Language:               "ja",
		Timeout:                30,
		RateLimit:              1000,
		RetryCount:             3,
		UserAgent:              "test-agent",
		Proxy:                  &ProxyConfig{Enabled: true, Profile: "p1"},
		DownloadProxy:          &ProxyConfig{Enabled: true},
		BaseURL:                "https://example.com",
		UseFlareSolverr:        true,
		UseBrowser:             true,
		ScrapeActress:          boolPtr(true),
		Cookies:                map[string]string{"session": "abc"},
		PlaceholderThresholdKB: 50,
		ExtraPlaceholderHashes: []string{"hash1", "hash2"},
		ScrapeBonusScreens:     true,
		APIKey:                 "key123",
	}

	data, err := json.Marshal(s)
	require.NoError(t, err)

	// Unmarshal back and verify key fields
	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, true, result["enabled"])
	assert.Equal(t, "ja", result["language"])
	assert.Equal(t, "test-agent", result["user_agent"])
	assert.Equal(t, "https://example.com", result["base_url"])
	assert.NotNil(t, result["proxy"])
	assert.NotNil(t, result["download_proxy"])
	assert.Equal(t, true, result["scrape_actress"])
	assert.NotNil(t, result["cookies"])
	assert.Equal(t, float64(50), result["placeholder_threshold"])
	assert.NotNil(t, result["extra_placeholder_hashes"])
	assert.Equal(t, true, result["scrape_bonus_screens"])
	assert.Equal(t, "key123", result["api_key"])
}

func TestScraperSettings_MarshalJSON_Nil(t *testing.T) {
	var s *ScraperSettings
	data, err := json.Marshal(s)
	require.NoError(t, err)
	assert.Equal(t, "null", string(data))
}

func TestScraperSettings_MarshalJSON_Minimal(t *testing.T) {
	s := &ScraperSettings{
		Enabled:   false,
		Language:  "en",
		Timeout:   10,
		RateLimit: 500,
		UserAgent: "ua",
	}

	data, err := json.Marshal(s)
	require.NoError(t, err)

	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	// Optional fields should not be present
	_, hasProxy := result["proxy"]
	assert.False(t, hasProxy, "proxy should be absent when nil")
	_, hasDownloadProxy := result["download_proxy"]
	assert.False(t, hasDownloadProxy, "download_proxy should be absent when nil")
	_, hasBaseURL := result["base_url"]
	assert.False(t, hasBaseURL, "base_url should be absent when empty")
	_, hasScrapeActress := result["scrape_actress"]
	assert.False(t, hasScrapeActress, "scrape_actress should be absent when nil")
	_, hasCookies := result["cookies"]
	assert.False(t, hasCookies, "cookies should be absent when nil")
	_, hasPlaceholderThreshold := result["placeholder_threshold"]
	assert.False(t, hasPlaceholderThreshold, "placeholder_threshold should be absent when 0")
	_, hasExtraPlaceholderHashes := result["extra_placeholder_hashes"]
	assert.False(t, hasExtraPlaceholderHashes, "extra_placeholder_hashes should be absent when nil")
	_, hasScrapeBonusScreens := result["scrape_bonus_screens"]
	assert.False(t, hasScrapeBonusScreens, "scrape_bonus_screens should be absent when false")
	_, hasAPIKey := result["api_key"]
	assert.False(t, hasAPIKey, "api_key should be absent when empty")
}

func boolPtr(b bool) *bool { return &b }
