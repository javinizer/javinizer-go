package javlibrary

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests use the documented ParseHTML seam to verify HTML parsing
// instead of reaching into unexported helper functions.

func newTestScraper() *scraper {
	return newScraper(&models.ScraperSettings{Enabled: true, RateLimit: 100}, nil, models.FlareSolverrConfig{})
}

func TestParseHTML_ExtractsID(t *testing.T) {
	s := newTestScraper()
	html := `<html><title>IPX-535 Test Movie - JAVLibrary</title></html>`
	result, err := s.ParseHTMLRaw(html, "IPX-535", "https://www.javlibrary.com/en/?v=abc123", "en")
	require.NoError(t, err)
	assert.Equal(t, "IPX-535", result.ID)
}

func TestParseHTML_ExtractsTitle(t *testing.T) {
	s := newTestScraper()
	html := `<html><title>IPX-535 Test Movie - JAVLibrary</title></html>`
	result, err := s.ParseHTMLRaw(html, "IPX-535", "https://www.javlibrary.com/en/?v=abc123", "en")
	require.NoError(t, err)
	assert.Contains(t, result.Title, "Test Movie")
}

func TestParseHTML_ExtractsReleaseDate(t *testing.T) {
	s := newTestScraper()
	html := `<html><title>IPX-535 Test - JAVLibrary</title>
		<div id="video_date"><td class="text">2023-01-15</td></div></html>`
	result, err := s.ParseHTMLRaw(html, "IPX-535", "https://www.javlibrary.com/en/?v=abc123", "en")
	require.NoError(t, err)
	assert.NotNil(t, result.ReleaseDate)
}

func TestParseHTML_ExtractsMaker(t *testing.T) {
	s := newTestScraper()
	html := `<html><title>IPX-535 Test - JAVLibrary</title>
		<div id="video_maker"><a href="/cn/?v=m123">Studio A</a></div></html>`
	result, err := s.ParseHTMLRaw(html, "IPX-535", "https://www.javlibrary.com/en/?v=abc123", "en")
	require.NoError(t, err)
	assert.Equal(t, "Studio A", result.Maker)
}

func TestParseHTML_ExtractsGenres(t *testing.T) {
	s := newTestScraper()
	html := `<html><title>IPX-535 Test - JAVLibrary</title>
		<span class="genre"><a href="/genre/1" rel="tag">Genre1</a></span>
		<span class="genre"><a href="/genre/2" rel="tag">Genre2</a></span></html>`
	result, err := s.ParseHTMLRaw(html, "IPX-535", "https://www.javlibrary.com/en/?v=abc123", "en")
	require.NoError(t, err)
	assert.Equal(t, []string{"Genre1", "Genre2"}, result.Genres)
}
