package javbus

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

func TestParseHTML_ExtractsIDFromInfoSection(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true, RateLimit: 100}, nil, models.FlareSolverrConfig{})

	html := `<html>
	<div id="info">
		<p><span class="header">品番:</span>IPX-535</p>
		<p><span class="header">発売日:</span> 2023-01-15</p>
		<p><span class="header">メーカー:</span><a href="/maker/1">Studio A</a></p>
	</div>
	</html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result, err := s.ParseHTML(doc, "https://www.javbus.com/IPX-535")
	require.NoError(t, err)
	assert.Equal(t, "IPX-535", result.ID)
}

func TestParseHTML_ExtractsMakerFromLinkValue(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true, RateLimit: 100}, nil, models.FlareSolverrConfig{})

	html := `<html>
	<div id="info">
		<p><span class="header">品番:</span>IPX-535</p>
		<p><span class="header">メーカー:</span><a href="/maker/1">Studio A</a></p>
	</div>
	</html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result, err := s.ParseHTML(doc, "https://www.javbus.com/IPX-535")
	require.NoError(t, err)
	assert.Equal(t, "Studio A", result.Maker)
}

func TestParseHTML_NoMatchReturnsEmptyID(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true, RateLimit: 100}, nil, models.FlareSolverrConfig{})

	html := `<html><div id="info"><p>Other: value</p></div></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result, err := s.ParseHTML(doc, "https://www.javbus.com/unknown")
	require.NoError(t, err)
	assert.Equal(t, "", result.ID)
}
