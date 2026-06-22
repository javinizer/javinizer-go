package javlibrary

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	t.Run("no flaresolverr", func(t *testing.T) {
		s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
		assert.NoError(t, s.Close())
	})
	t.Run("close twice no panic", func(t *testing.T) {
		s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
		assert.NoError(t, s.Close())
		assert.NoError(t, s.Close())
	})
}

func TestIsValidLanguage(t *testing.T) {
	tests := []struct {
		lang     string
		expected bool
	}{
		{"en", true},
		{"ja", true},
		{"cn", true},
		{"tw", true},
		{"", false},
		{"fr", false},
		{"EN", false}, // case-sensitive
	}

	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			assert.Equal(t, tt.expected, isValidLanguage(tt.lang))
		})
	}
}

func TestScraper_ResolveDownloadProxyForHost_Uncovered(t *testing.T) {
	dlProxy := &models.ProxyConfig{Enabled: true, Profile: "dl"}
	s := newScraper(&models.ScraperSettings{Enabled: true, DownloadProxy: dlProxy}, nil, models.FlareSolverrConfig{})

	t.Run("javlibrary.com returns proxy", func(t *testing.T) {
		dl, _, ok := s.ResolveDownloadProxyForHost("javlibrary.com")
		assert.True(t, ok)
		assert.Equal(t, dlProxy, dl)
	})
	t.Run("c.impact.jp returns proxy", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("c.impact.jp")
		assert.True(t, ok)
	})
	t.Run("subdomain of c.impact.jp returns proxy", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("pics.c.impact.jp")
		assert.True(t, ok)
	})
	t.Run("unrelated returns false", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("example.com")
		assert.False(t, ok)
	})
	t.Run("empty returns false", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("")
		assert.False(t, ok)
	})
}
