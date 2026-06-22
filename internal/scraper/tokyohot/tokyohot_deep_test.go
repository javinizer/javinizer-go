package tokyohot

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
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

func TestParseHTML_ExtractsTitle(t *testing.T) {
	html := `<html><head><title>Test Movie Title | TokyoHot</title></head><body></body></html>`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, "https://www.tokyohot.com/product/?q=n1234", "ja")
	assert.Equal(t, "Test Movie Title", result.Title)
}

func TestParseHTML_ExtractsReleaseDate(t *testing.T) {
	html := `<html><body>
		<dl class="info"><dt>Release Date</dt><dd>2023-01-15</dd></dl>
		</body></html>`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, "https://www.tokyohot.com/product/?q=n1234", "ja")
	assert.NotNil(t, result.ReleaseDate)
}

func TestParseHTML_ExtractsID(t *testing.T) {
	html := `<html><body>
		<dl class="info"><dt>Product ID</dt><dd>n1234</dd></dl>
		</body></html>`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, "https://www.tokyohot.com/product/?q=n1234", "ja")
	assert.Equal(t, "N1234", result.ID)
}
