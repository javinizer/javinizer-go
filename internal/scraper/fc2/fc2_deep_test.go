package fc2

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

func TestParseHTML_ExtractsIDFromURL(t *testing.T) {
	html := `<html><meta property="og:title" content="FC2-PPV-1234567 Test Movie"></html>`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, html, "https://adult.contents.fc2.com/article/1234567/")
	assert.Equal(t, "FC2-PPV-1234567", result.ID)
}

func TestParseHTML_ExtractsTitle(t *testing.T) {
	html := `<html><meta property="og:title" content="FC2-PPV-1234567 Test Movie"></html>`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, html, "https://adult.contents.fc2.com/article/1234567/")
	assert.Contains(t, result.Title, "Test Movie")
}

func TestParseHTML_ExtractsReleaseDate(t *testing.T) {
	html := `<html><meta property="og:title" content="FC2-1234567 Title">
		<div class="items_article_softDevice"><p>販売日：2023/01/15</p></div></html>`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, html, "https://adult.contents.fc2.com/article/1234567/")
	assert.NotNil(t, result.ReleaseDate)
}

func TestParseHTML_ExtractsMaker(t *testing.T) {
	html := `<html><meta property="og:title" content="FC2-1234567 Title">
		<div class="items_article_headerInfo"><a href="/users/123">Studio A</a></div></html>`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, html, "https://adult.contents.fc2.com/article/1234567/")
	assert.Equal(t, "Studio A", result.Maker)
}

func TestParseHTML_ExtractsCoverFromOGImage(t *testing.T) {
	html := `<html><meta property="og:title" content="FC2-1234567 Title">
		<meta property="og:image" content="https://example.com/cover.jpg"></html>`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, html, "https://adult.contents.fc2.com/article/1234567/")
	assert.Equal(t, "https://example.com/cover.jpg", result.CoverURL)
}

func TestParseHTML_ReturnsNilWhenNoID(t *testing.T) {
	html := `<html><body>No product info</body></html>`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, html, "https://adult.contents.fc2.com/")
	assert.Nil(t, result)
}
