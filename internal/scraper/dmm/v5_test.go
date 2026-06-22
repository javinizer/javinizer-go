package dmm

import (
	"context"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestURLPriorityV5 tests URL priority assignment
func TestURLPriorityV5(t *testing.T) {
	tests := []struct {
		url      string
		expected int
	}{
		{"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=ipx999/", 350},
		{"https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx999/", 300},
		{"https://www.dmm.co.jp/digital/videoc/-/detail/=/cid=abc123/", 300},
		{"https://video.dmm.co.jp/amateur/content/123/", 250},
		{"https://video.dmm.co.jp/av/content/123/", 200},
		{"https://www.dmm.co.jp/monthly/premium/-/detail/=/cid=ipx999/", 150},
		{"https://www.dmm.co.jp/monthly/standard/-/detail/=/cid=ipx999/", 100},
		{"https://www.dmm.co.jp/rental/-/detail/=/cid=ipx999/", 0},
		{"https://www.dmm.co.jp/unknown/path/", 0},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			result := urlPriority(tt.url)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMaxPriorityV5 tests max priority finding
func TestMaxPriorityV5(t *testing.T) {
	candidates := []urlCandidate{
		{url: "https://www.dmm.co.jp/rental/", priority: 0},
		{url: "https://www.dmm.co.jp/digital/videoa/", priority: 300},
		{url: "https://www.dmm.co.jp/monthly/premium/", priority: 150},
	}
	assert.Equal(t, 300, maxPriority(candidates))
	assert.Equal(t, 0, maxPriority(nil))
}

// TestSortCandidatesV5 tests candidate sorting
func TestSortCandidatesV5(t *testing.T) {
	candidates := []urlCandidate{
		{url: "low", priority: 100, idLength: 5},
		{url: "high", priority: 300, idLength: 7},
		{url: "mid", priority: 200, idLength: 3},
	}
	sortCandidates(candidates)
	assert.Equal(t, 300, candidates[0].priority)
	assert.Equal(t, 200, candidates[1].priority)
	assert.Equal(t, 100, candidates[2].priority)
}

// TestSortCandidatesV5_SamePriority tests sorting with same priority
func TestSortCandidatesV5_SamePriority(t *testing.T) {
	candidates := []urlCandidate{
		{url: "long", priority: 100, idLength: 10},
		{url: "short", priority: 100, idLength: 5},
	}
	sortCandidates(candidates)
	assert.Equal(t, 5, candidates[0].idLength) // Shorter ID first
}

// TestScrapeURLV5_NonDMMURL tests ScrapeURL rejecting non-DMM URLs
func TestScrapeURLV5_NonDMMURL(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.ScrapeURL(context.Background(), "https://example.com/movies/123")
	assert.Nil(t, result)
	assert.Error(t, err)
}

// TestCanHandleURLV5 tests CanHandleURL
func TestCanHandleURLV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	tests := []struct {
		url      string
		expected bool
	}{
		{"https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00999/", true},
		{"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=abc12345/", true},
		{"https://example.com/movies/123", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.expected, s.CanHandleURL(tt.url))
		})
	}
}

// TestNewScraperV5 tests scraper creation
func TestNewScraperV5(t *testing.T) {
	settings := &models.ScraperSettings{
		Enabled: true,
	}

	s := newScraper(settings, nil, models.FlareSolverrConfig{}, dmmOptions{})
	require.NotNil(t, s)
	assert.True(t, s.enabled)
}

// TestResolveTimeoutV5 tests timeout resolution
func TestResolveTimeoutV5(t *testing.T) {
	assert.Equal(t, 10, resolveTimeout(10, 30))
	assert.Equal(t, 30, resolveTimeout(0, 30))
	assert.Equal(t, 30, resolveTimeout(0, 0))
	assert.Equal(t, 5, resolveTimeout(5, 0))
}

// TestExtractContentIDFromURLV5 tests URL content ID extraction
func TestExtractContentIDFromURLV5(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{"with cid param", "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00999/", "ipx00999"},
		{"no cid", "https://www.dmm.co.jp/", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractContentIDFromURL(tt.url)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestResolveDownloadProxyForHostV5 tests proxy resolution
func TestResolveDownloadProxyForHostV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	t.Run("dmm.co.jp", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("dmm.co.jp")
		assert.True(t, ok)
	})

	t.Run("empty", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("")
		assert.False(t, ok)
	})

	t.Run("unrelated", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("example.com")
		assert.False(t, ok)
	})
}

// TestParseHTMLV5_Basic tests HTML parsing - skipped due to complex dependencies
func TestParseHTMLV5_Basic(t *testing.T) {
	t.Skip("requires complex DMM HTML fixtures and dependencies")
}

// TestSearchV5_WithHTTPError tests Search with HTTP error - skipped
func TestSearchV5_WithHTTPError(t *testing.T) {
	t.Skip("requires complex DMM URL resolution setup")
}
