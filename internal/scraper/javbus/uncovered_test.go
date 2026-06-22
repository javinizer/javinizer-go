package javbus

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScraper_IsEnabled_Uncovered(t *testing.T) {
	t.Run("enabled", func(t *testing.T) {
		s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
		assert.True(t, s.IsEnabled())
	})
	t.Run("disabled", func(t *testing.T) {
		s := newScraper(&models.ScraperSettings{Enabled: false}, nil, models.FlareSolverrConfig{})
		assert.False(t, s.IsEnabled())
	})
}

func TestScraper_Config_Uncovered(t *testing.T) {
	settings := &models.ScraperSettings{Enabled: true, Timeout: 30}
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	cfg := s.Config()
	require.NotNil(t, cfg)
	assert.True(t, cfg.Enabled)
	assert.Equal(t, 30, cfg.Timeout)
	// Verify it's a clone
	cfg.Enabled = false
	assert.True(t, s.Config().Enabled)
}

func TestScraper_Close_Uncovered(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	assert.NoError(t, s.Close())
}

func TestValidateScraperSettings_Uncovered(t *testing.T) {
	t.Run("valid enabled config", func(t *testing.T) {
		assert.NoError(t, validateScraperSettings(&models.ScraperSettings{Enabled: true, RateLimit: 1000, RetryCount: 3, Timeout: 30}))
	})

	t.Run("zero values valid", func(t *testing.T) {
		assert.NoError(t, validateScraperSettings(&models.ScraperSettings{Enabled: true, RateLimit: 0, RetryCount: 0, Timeout: 0}))
	})
}

func TestScraper_ResolveDownloadProxyForHost_Uncovered(t *testing.T) {
	dlProxy := &models.ProxyConfig{Enabled: true, Profile: "dl"}
	s := newScraper(&models.ScraperSettings{Enabled: true, DownloadProxy: dlProxy}, nil, models.FlareSolverrConfig{})

	t.Run("javbus.com returns proxy", func(t *testing.T) {
		dl, _, ok := s.ResolveDownloadProxyForHost("javbus.com")
		assert.True(t, ok)
		assert.Equal(t, dlProxy, dl)
	})
	t.Run("javbus.org returns proxy", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("javbus.org")
		assert.True(t, ok)
	})
	t.Run("subdomain javbus.com returns proxy", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("pics.javbus.com")
		assert.True(t, ok)
	})
	t.Run("subdomain javbus.org returns proxy", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("pics.javbus.org")
		assert.True(t, ok)
	})
	t.Run("unrelated host returns false", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("example.com")
		assert.False(t, ok)
	})
	t.Run("empty host returns false", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("")
		assert.False(t, ok)
	})
}

func TestNormalizeLanguage_Uncovered(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "zh"},
		{"ja", "ja"},
		{"en", "en"},
		{"zh", "zh"},
		{"cn", "zh"},
		{"tw", "zh"},
		{"JP", "zh"},
		{"unknown", "zh"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeLanguage(tt.input))
		})
	}
}

func TestIdsMatch_Uncovered(t *testing.T) {
	assert.True(t, idsMatch("ABC-123", "abc123"))
	assert.True(t, idsMatch("ABC-123", "abc"))
	assert.True(t, idsMatch("ABC", "abc123"))
	assert.False(t, idsMatch("", "abc123"))
	assert.False(t, idsMatch("XYZ-789", "abc123"))
}

func TestIsLikelyImageURL_Uncovered(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		{"jpg", "https://example.com/image.jpg", true},
		{"png", "https://example.com/image.png", true},
		{"webp", "https://example.com/image.webp", true},
		{"html", "https://example.com/page.html", false},
		{"empty", "", false},
		{"no extension", "https://example.com/image", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isLikelyImageURL(tt.url))
		})
	}
}
