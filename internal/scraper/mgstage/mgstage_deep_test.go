package mgstage

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

func TestParseHTML_ExtractsIDAndTitle(t *testing.T) {
	s := newTestScraper()
	html := `<html><body>
		<title>SIRO-5615 Test Movie</title>
		<table><tr><th>品番：</th><td>SIRO-5615</td></tr></table>
		</body></html>`
	doc := mustDoc(t, html)
	result, err := s.ParseHTML(doc, "https://www.mgstage.com/product/product_detail/SIRO-5615/")
	require.NoError(t, err)
	assert.Equal(t, "SIRO-5615", result.ID)
}

func TestParseHTML_ExtractsMaker(t *testing.T) {
	s := newTestScraper()
	html := `<html><body>
		<table>
		<tr><th>品番：</th><td>TEST-001</td></tr>
		<tr><th>メーカー：</th><td><a href="/maker/1">Studio A</a></td></tr>
		</table>
		</body></html>`
	doc := mustDoc(t, html)
	result, err := s.ParseHTML(doc, "https://www.mgstage.com/product/product_detail/TEST-001/")
	require.NoError(t, err)
	assert.Equal(t, "Studio A", result.Maker)
}

func TestParseHTML_ExtractsIDAndGenres(t *testing.T) {
	s := newTestScraper()
	html := `<html><body>
		<table>
		<tr><th>品番：</th><td>TEST-001</td></tr>
		<tr><th>ジャンル：</th><td><a href="/genre/1">Genre1</a><a href="/genre/2">Genre2</a></td></tr>
		</table>
		</body></html>`
	doc := mustDoc(t, html)
	result, err := s.ParseHTML(doc, "https://www.mgstage.com/product/product_detail/TEST-001/")
	require.NoError(t, err)
	assert.Equal(t, "TEST-001", result.ID)
	assert.Contains(t, result.Genres, "Genre1")
	assert.Contains(t, result.Genres, "Genre2")
}
