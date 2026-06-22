package javdb

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests use the documented ParseHTML seam to verify HTML parsing
// instead of reaching into unexported helper functions.

func mustDoc(t *testing.T, html string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)
	return doc
}

func newTestScraper() *scraper {
	return newScraper(&models.ScraperSettings{Enabled: true, RateLimit: 100}, nil, models.FlareSolverrConfig{})
}

func TestParseHTML_ExtractsID(t *testing.T) {
	s := newTestScraper()
	html := `<html><body>
		<div class="movie-panel-info">
			<div class="panel-block"><strong>番號</strong><span class="value">IPX-535</span></div>
		</div></body></html>`
	doc := mustDoc(t, html)
	result, err := s.ParseHTML(doc, "https://javdb.com/v/abc123")
	require.NoError(t, err)
	assert.Equal(t, "IPX-535", result.ID)
}

func TestParseHTML_ExtractsTitle(t *testing.T) {
	s := newTestScraper()
	html := `<html><body>
		<h2 class="title is-4"><strong>IPX-535</strong> Test Movie Title</h2>
		</body></html>`
	doc := mustDoc(t, html)
	result, err := s.ParseHTML(doc, "https://javdb.com/v/abc123")
	require.NoError(t, err)
	assert.Contains(t, result.Title, "Test Movie Title")
}

func TestParseHTML_ExtractsReleaseDate(t *testing.T) {
	s := newTestScraper()
	html := `<html><body>
		<div class="movie-panel-info">
			<div class="panel-block"><strong>日期</strong><span class="value">2023-01-15</span></div>
		</div></body></html>`
	doc := mustDoc(t, html)
	result, err := s.ParseHTML(doc, "https://javdb.com/v/abc123")
	require.NoError(t, err)
	assert.NotNil(t, result.ReleaseDate)
}

func TestParseHTML_ExtractsRuntime(t *testing.T) {
	s := newTestScraper()
	html := `<html><body>
		<div class="movie-panel-info">
			<div class="panel-block"><strong>時長</strong><span class="value">120</span></div>
		</div></body></html>`
	doc := mustDoc(t, html)
	result, err := s.ParseHTML(doc, "https://javdb.com/v/abc123")
	require.NoError(t, err)
	assert.Equal(t, 120, result.Runtime)
}

func TestParseHTML_ExtractsMaker(t *testing.T) {
	s := newTestScraper()
	html := `<html><body>
		<div class="movie-panel-info">
			<div class="panel-block"><strong>片商</strong><span class="value"><a href="/makers/1">Studio A</a></span></div>
		</div></body></html>`
	doc := mustDoc(t, html)
	result, err := s.ParseHTML(doc, "https://javdb.com/v/abc123")
	require.NoError(t, err)
	assert.Equal(t, "Studio A", result.Maker)
}
