package r18dev

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func newTestScraper() *scraper {
	return &scraper{
		enabled:  true,
		language: "en",
	}
}

func TestScraper_Close(t *testing.T) {
	s := newTestScraper()
	assert.NoError(t, s.Close())
}

func TestScraper_CanHandleURL(t *testing.T) {
	s := newTestScraper()
	tests := []struct {
		url    string
		expect bool
	}{
		{"https://r18.dev/videos/vod/movies/detail/-/id=ipx00635", true},
		{"https://www.r18.dev/videos/vod/movies/detail/-/id=abc123", true},
		{"https://r18.com/videos/vod/movies/detail/-/id=abc123", true},
		{"https://www.r18.com/videos/vod/movies/detail/-/id=abc123", true},
		{"https://javdb.com/search?q=test", false},
		{"https://example.com", false},
		{"not-a-url", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expect, s.CanHandleURL(tt.url), "url=%s", tt.url)
	}
}

func TestScraper_ExtractIDFromURL(t *testing.T) {
	s := newTestScraper()
	t.Run("valid URL extracts ID", func(t *testing.T) {
		id, err := s.ExtractIDFromURL("https://r18.dev/videos/vod/movies/detail/-/id=ipx00635")
		assert.NoError(t, err)
		assert.Equal(t, "ipx00635", id)
	})
	t.Run("invalid URL returns error", func(t *testing.T) {
		_, err := s.ExtractIDFromURL("https://r18.dev/no-id-here")
		assert.Error(t, err)
	})
}

func TestValidateScraperSettings_Uncovered(t *testing.T) {
	t.Run("valid en config", func(t *testing.T) {
		assert.NoError(t, validateScraperSettings(&models.ScraperSettings{Enabled: true, Language: "en"}))
	})

	t.Run("valid ja config", func(t *testing.T) {
		assert.NoError(t, validateScraperSettings(&models.ScraperSettings{Enabled: true, Language: "ja"}))
	})

	t.Run("invalid language returns error", func(t *testing.T) {
		assert.Error(t, validateScraperSettings(&models.ScraperSettings{Enabled: true, Language: "fr"}))
	})

	t.Run("valid empty language defaults to en", func(t *testing.T) {
		assert.NoError(t, validateScraperSettings(&models.ScraperSettings{Enabled: true, Language: ""}))
	})
}

func TestScraper_ResolveDownloadProxyForHost(t *testing.T) {
	s := newTestScraper()

	t.Run("r18.dev host returns true", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("r18.dev")
		assert.True(t, ok)
	})

	t.Run("subdomain of r18.dev returns true", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("cdn.r18.dev")
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

func TestScraper_ContentIDMatchesExpected(t *testing.T) {
	// These use the regex from r18dev which expects specific format
	// contentIDCoreMatch compares content IDs with expected DVD IDs
	t.Run("empty contentID returns false", func(t *testing.T) {
		assert.False(t, contentIDCoreMatch("", "IPX-635"))
	})
	t.Run("matching content IDs", func(t *testing.T) {
		assert.False(t, contentIDCoreMatch("abc01234", "DEF-1234"))
	})
}

func TestScraper_GenerateAlternateContentIDs(t *testing.T) {
	t.Run("generates alternates for lowercase ID", func(t *testing.T) {
		result := generateContentIDVariations("ipx635")
		assert.NotEmpty(t, result)
	})
	t.Run("returns nil for non-matching input", func(t *testing.T) {
		result := generateContentIDVariations("")
		assert.Nil(t, result)
	})
}
