package jav321

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScraper_Name_Uncovered(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	assert.Equal(t, "jav321", s.Name())
}

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

func TestScraper_ResolveDownloadProxyForHost_Uncovered(t *testing.T) {
	dlProxy := &models.ProxyConfig{Enabled: true, Profile: "dl"}
	s := newScraper(&models.ScraperSettings{Enabled: true, DownloadProxy: dlProxy}, nil, models.FlareSolverrConfig{})

	t.Run("jav321.com returns proxy", func(t *testing.T) {
		dl, _, ok := s.ResolveDownloadProxyForHost("jav321.com")
		assert.True(t, ok)
		assert.Equal(t, dlProxy, dl)
	})
	t.Run("subdomain returns proxy", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("www.jav321.com")
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
		{"ABC-123", "ABC-123"},
		{"abc123", "ABC123"},
		{"no match", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractID(tt.input))
		})
	}
}

func TestStripTrailingID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ABC-123 Some Title", "Some Title"},
		{"Title Only", "Title Only"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, stripTrailingID(tt.input))
		})
	}
}

func TestStripTrailingSiteName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Some Title - JAV321", "Some Title"},
		{"Some Title | JAV321", "Some Title"},
		{"Some Title - Jav321", "Some Title"},
		{"Clean Title", "Clean Title"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, stripTrailingSiteName(tt.input))
		})
	}
}

func TestStripTags_Uncovered(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"<b>bold</b>", "bold"},
		{"<a href=\"/x\">link</a>", "link"},
		{"no tags", "no tags"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, stripTags(tt.input))
		})
	}
}

func TestIsUsableDescription(t *testing.T) {
	assert.False(t, isUsableDescription(""))
	assert.False(t, isUsableDescription("short")) // too short
	assert.False(t, isUsableDescription("adsbyjuicy ad content here that is long enough to pass"))
	assert.True(t, isUsableDescription("This is a valid description of a movie that has enough content"))
}

func TestSplitValues(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"A, B, C", []string{"A", "B", "C"}},
		{"A、B、C", []string{"A", "B", "C"}},
		{"A/B/C", []string{"A", "B", "C"}},
		{"", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, splitValues(tt.input))
		})
	}
}
