package dlgetchu

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

func TestParseHTML_ExtractsIDFromProductID(t *testing.T) {
	html := `作品ID：123456 <meta property="og:title" content="Test Title"> 2023/01/15 90分`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, html, "http://dl.getchu.com/i/item123456")
	assert.Equal(t, "123456", result.ID)
}

func TestParseHTML_ExtractsTitleFromOGMeta(t *testing.T) {
	html := `<meta property="og:title" content="Test Title">`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, html, "http://dl.getchu.com/i/item123456")
	assert.Equal(t, "Test Title", result.Title)
}

func TestParseHTML_ExtractsRuntime(t *testing.T) {
	html := `９０分 <meta property="og:title" content="Title">`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, html, "http://dl.getchu.com/i/item123456")
	assert.Equal(t, 90, result.Runtime)
}

func TestParseHTML_ExtractsMaker(t *testing.T) {
	html := `<a href="dojin_circle_detail.php?id=1">Maker A</a> <meta property="og:title" content="Title">`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, html, "http://dl.getchu.com/i/item123456")
	assert.Equal(t, "Maker A", result.Maker)
}

func TestParseHTML_ExtractsDescription(t *testing.T) {
	html := `作品内容</td><td>This is a detailed description.</td> <meta property="og:title" content="Title">`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, html, "http://dl.getchu.com/i/item123456")
	assert.Equal(t, "This is a detailed description.", result.Description)
}

func TestParseHTML_FallbackToTitleTag(t *testing.T) {
	html := `<title>Page Title</title>`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, html, "http://dl.getchu.com/i/item123456")
	assert.Equal(t, "Page Title", result.Title)
}
