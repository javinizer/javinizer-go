package aventertainment

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

func TestParseHTML_ExtractsIDFromTagTitle(t *testing.T) {
	html := `<span class="tag-title">ABP-420</span> <meta property="og:title" content="Test Title">`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, html, "https://www.aventertainments.com/ppv/detail/123", "en", false)
	assert.Equal(t, "ABP-420", result.ID)
}

func TestParseHTML_ExtractsTitle(t *testing.T) {
	html := `<meta property="og:title" content="Test Movie Title">`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, html, "https://www.aventertainments.com/ppv/detail/123", "en", false)
	assert.Contains(t, result.Title, "Test Movie Title")
}

func TestParseHTML_ExtractsReleaseDate(t *testing.T) {
	html := `<span class="title">発売日</span><span class="value">01/15/2023</span> <meta property="og:title" content="Title">`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, html, "https://www.aventertainments.com/ppv/detail/123", "en", false)
	assert.NotNil(t, result.ReleaseDate)
}

func TestParseHTML_ExtractsRuntime(t *testing.T) {
	html := `<span class="title">収録時間</span><span class="value">1:30</span> <meta property="og:title" content="Title">`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, html, "https://www.aventertainments.com/ppv/detail/123", "en", false)
	assert.Equal(t, 90, result.Runtime)
}

func TestParseHTML_ExtractsMaker(t *testing.T) {
	html := `<a href="ppv_studioproducts?StudioID=1">Studio A</a> <meta property="og:title" content="Title">`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, html, "https://www.aventertainments.com/ppv/detail/123", "en", false)
	assert.Equal(t, "Studio A", result.Maker)
}

func TestParseHTML_ExtractsDescription(t *testing.T) {
	html := `<div class="product-description">A real movie synopsis with enough content to be meaningful.</div> <meta property="og:title" content="Title">`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, html, "https://www.aventertainments.com/ppv/detail/123", "en", false)
	assert.Equal(t, "A real movie synopsis with enough content to be meaningful.", result.Description)
}

func TestParseHTML_ExtractsGenres(t *testing.T) {
	html := `<div class="value-category"><a href="/cat/1">Genre1</a><a href="/dept/2">Genre2</a></div> <meta property="og:title" content="Title">`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, html, "https://www.aventertainments.com/ppv/detail/123", "en", false)
	assert.Contains(t, result.Genres, "Genre1")
	assert.Contains(t, result.Genres, "Genre2")
}

func TestParseHTML_ExtractsActresses(t *testing.T) {
	html := `<div><a href="/ppv_actressdetail/1">Actress1</a><a href="/ppv/idoldetail/2">Actress2</a></div> <meta property="og:title" content="Title">`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, html, "https://www.aventertainments.com/ppv/detail/123", "en", false)
	require.Len(t, result.Actresses, 2)
}
