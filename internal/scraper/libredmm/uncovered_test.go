package libredmm

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScraper_Name(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	assert.Equal(t, "libredmm", s.Name())
}

func TestScraper_IsEnabled(t *testing.T) {
	t.Run("enabled", func(t *testing.T) {
		s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
		assert.True(t, s.IsEnabled())
	})
	t.Run("disabled", func(t *testing.T) {
		s := newScraper(&models.ScraperSettings{Enabled: false}, nil, models.FlareSolverrConfig{})
		assert.False(t, s.IsEnabled())
	})
}

func TestScraper_Config(t *testing.T) {
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

func TestScraper_Close(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	assert.NoError(t, s.Close())
}

func TestScraper_CanHandleURL(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})

	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		{"libredmm.com", "https://libredmm.com/movies/ABC-123", true},
		{"subdomain", "https://www.libredmm.com/movies/ABC-123", true},
		{"unrelated", "https://example.com/movies/ABC-123", false},
		{"invalid URL", "://not-a-url", false},
		{"empty URL", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, s.CanHandleURL(tt.url))
		})
	}
}

func TestScraper_ExtractIDFromURL(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})

	tests := []struct {
		name        string
		url         string
		expectedID  string
		expectError bool
	}{
		{"movies path", "https://libredmm.com/movies/ABC-123", "ABC-123", false},
		{"movies path with json", "https://libredmm.com/movies/ABC-123.json", "ABC-123", false},
		{"cid path", "https://libredmm.com/cid/ABC-123", "ABC-123", false},
		{"search query", "https://libredmm.com/search?q=ABC-123", "ABC-123", false},
		{"invalid URL", "://not-a-url", "", true},
		{"no extractable ID", "https://libredmm.com/", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := s.ExtractIDFromURL(tt.url)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedID, id)
			}
		})
	}
}

func TestStripANSICodes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"plain text", "hello", "hello"},
		{"ANSI escape", "\x1b[31mhello\x1b[0m", "hello"},
		{"bare ESC char", "\x1bhello", "hello"},
		{"control chars", "\x00\x01hello", "hello"},
		{"JSON with prefix", "garbage{\"key\":\"val\"}", "{\"key\":\"val\"}"},
		{"JSON with prefix array", "prefix[1,2,3]", "[1,2,3]"},
		{"already JSON", "{\"key\":\"val\"}", "{\"key\":\"val\"}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, stripANSICodes(tt.input))
		})
	}
}

func TestNormalizeMovieURL_Uncovered(t *testing.T) {
	base := "https://libredmm.com"

	tests := []struct {
		name     string
		raw      string
		expectOK bool
	}{
		{"movies path", "https://libredmm.com/movies/ABC-123", true},
		{"cid path", "https://libredmm.com/cid/ABC-123", true},
		{"search query", "https://libredmm.com/search?q=ABC-123", true},
		{"non-http", "not-a-url", false},
		{"different host", "https://example.com/movies/ABC-123", false},
		{"empty path", "https://libredmm.com/", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := normalizeMovieURL(tt.raw, base)
			assert.Equal(t, tt.expectOK, ok)
		})
	}
}

func TestScraper_ResolveDownloadProxyForHost(t *testing.T) {
	dlProxy := &models.ProxyConfig{Enabled: true, Profile: "dl"}
	s := newScraper(&models.ScraperSettings{Enabled: true, DownloadProxy: dlProxy}, nil, models.FlareSolverrConfig{})

	t.Run("libredmm host returns proxy", func(t *testing.T) {
		dl, _, ok := s.ResolveDownloadProxyForHost("libredmm.com")
		assert.True(t, ok)
		assert.Equal(t, dlProxy, dl)
	})
	t.Run("subdomain returns proxy", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("www.libredmm.com")
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
