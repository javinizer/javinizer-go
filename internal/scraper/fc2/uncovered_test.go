package fc2

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

	t.Run("fc2.com returns proxy", func(t *testing.T) {
		dl, _, ok := s.ResolveDownloadProxyForHost("fc2.com")
		assert.True(t, ok)
		assert.Equal(t, dlProxy, dl)
	})
	t.Run("subdomain of fc2.com returns proxy", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("adult.contents.fc2.com")
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

func TestExtractArticleID_Uncovered(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://adult.contents.fc2.com/article/1234567/", "1234567"},
		{"FC2-PPV-1234567", "1234567"},
		{"PPV-1234567", "1234567"},
		{"1234567", "1234567"},
		{"no-match", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractArticleID(tt.input))
		})
	}
}

func TestCanonicalFC2ID_Uncovered(t *testing.T) {
	assert.Equal(t, "FC2-PPV-1234567", canonicalFC2ID("1234567"))
	assert.Equal(t, "FC2-PPV-123", canonicalFC2ID("123"))
}

func TestStripFC2IDPrefix_Uncovered(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"FC2-PPV-1234567 Title", "Title"},
		{"FC2 PPV 1234567 Title", "Title"},
		{"1234567 Title", "1234567 Title"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, stripFC2IDPrefix(tt.input))
		})
	}
}

func TestStripSiteSuffix_Uncovered(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Title | FC2 Content", "Title"},
		{"Title｜FC2 Content", "Title"},
		{"No suffix", "No suffix"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, stripSiteSuffix(tt.input))
		})
	}
}

func TestNormalizeURL_Uncovered(t *testing.T) {
	base := "https://adult.contents.fc2.com/article/1234567/"

	tests := []struct {
		name     string
		raw      string
		expected string
	}{
		{"absolute URL", "https://example.com/image.jpg", "https://example.com/image.jpg"},
		{"protocol-relative", "//example.com/image.jpg", "https://example.com/image.jpg"},
		{"relative path", "/img/photo.jpg", "https://adult.contents.fc2.com/img/photo.jpg"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeURL(tt.raw, base))
		})
	}
}

func TestBuildArticleURL(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	url := s.buildArticleURL("1234567")
	assert.Contains(t, url, "1234567")
	assert.Contains(t, url, "/article/")
}
