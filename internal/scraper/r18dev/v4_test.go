package r18dev

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestCanHandleURLV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	assert.True(t, s.CanHandleURL("https://r18.dev/videos/vod/movies/detail/-/id=ABC123"))
	assert.True(t, s.CanHandleURL("https://www.r18.dev/videos/vod/movies/detail/-/id=ABC123"))
	assert.False(t, s.CanHandleURL("https://example.com/detail/ABC123"))
	assert.False(t, s.CanHandleURL(""))
}

func TestContentIDMatchesExpectedV4(t *testing.T) {
	// contentIDCoreMatch uses contentIDFullRegex to parse IDs
	// Both IDs must match the pattern like "abc00123"
	assert.False(t, contentIDCoreMatch("", "ABC-123"))
	assert.False(t, contentIDCoreMatch("abc123", ""))
}

func TestNormalizeIDV4(t *testing.T) {
	// Test ID normalization
	result := normalizeID("61MDB087")
	assert.NotEmpty(t, result)
}

func TestScrapeURLV4_Disabled(t *testing.T) {
	s := &scraper{
		enabled:  false,
		settings: models.ScraperSettings{Enabled: false},
	}

	result, err := s.ScrapeURL(nil, "https://r18.dev/videos/vod/movies/detail/-/id=ABC123")
	assert.Nil(t, result)
	assert.Error(t, err)
}
