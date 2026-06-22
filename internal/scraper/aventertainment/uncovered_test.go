package aventertainment

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
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	assert.NoError(t, s.Close())
}

func TestScraper_ResolveDownloadProxyForHost_Uncovered(t *testing.T) {
	dlProxy := &models.ProxyConfig{Enabled: true, Profile: "dl"}
	s := newScraper(&models.ScraperSettings{Enabled: true, DownloadProxy: dlProxy}, nil, models.FlareSolverrConfig{})

	t.Run("aventertainments.com returns proxy", func(t *testing.T) {
		dl, _, ok := s.ResolveDownloadProxyForHost("aventertainments.com")
		assert.True(t, ok)
		assert.Equal(t, dlProxy, dl)
	})
	t.Run("subdomain returns proxy", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("www.aventertainments.com")
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

func TestExtractID_Uncovered(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ABCD-123", "ABCD-123"},
		{"1pon-012345-001", "1PON-012345-001"},
		{"carib-012345-001", "CARIB-012345-001"},
		{"abcd1234", "ABCD1234"},
		{"no-match", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractID(tt.input))
		})
	}
}

func TestNormalizeComparableID_Uncovered(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ABCD-123", "abcd123"},
		{"dlABCD-123", "abcd123"},
		{"stABCD-123", "abcd123"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeComparableID(tt.input))
		})
	}
}

func TestStripSiteSuffix_Uncovered(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Title - AV Entertainment", "Title"},
		{"Title | AV Entertainment", "Title"},
		{"Title | AV ENTERTAINMENT PAY-PER-VIEW", "Title"},
		{"No suffix", "No suffix"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, stripSiteSuffix(tt.input))
		})
	}
}
