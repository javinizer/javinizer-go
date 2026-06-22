package jav321

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

func TestParseHTML_ExtractsIDFromLabel(t *testing.T) {
	html := `<html><body>
		<div class="panel-heading"><h3>ABP-420 Test Movie</h3></div>
		<b>品番</b> : ABP-420<br>
		</body></html>`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, "https://jav321.com/movie/abp-420")
	assert.Equal(t, "ABP-420", result.ID)
}

func TestParseHTML_ExtractsTitle(t *testing.T) {
	html := `<html><body>
		<div class="panel-heading"><h3>Test Movie Title</h3></div>
		</body></html>`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, "https://jav321.com/movie/abp-420")
	assert.Contains(t, result.Title, "Test Movie Title")
}

func TestParseHTML_ExtractsReleaseDate(t *testing.T) {
	html := `<html><body>
		<div class="panel-heading"><h3>Title</h3></div>
		<b>発売日</b> : 2023-01-15<br>
		</body></html>`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, "https://jav321.com/movie/abp-420")
	assert.NotNil(t, result.ReleaseDate)
}

func TestParseHTML_ExtractsRuntime(t *testing.T) {
	html := `<html><body>
		<div class="panel-heading"><h3>Title</h3></div>
		<b>収録時間</b> : 120分<br>
		</body></html>`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, "https://jav321.com/movie/abp-420")
	assert.Equal(t, 120, result.Runtime)
}

func TestParseHTML_ExtractsMaker(t *testing.T) {
	html := `<html><body>
		<div class="panel-heading"><h3>Title</h3></div>
		<b>メーカー</b> : <a href="/maker/1">Studio A</a><br>
		</body></html>`
	doc := mustDoc(t, html)
	result := ParseHTML(doc, "https://jav321.com/movie/abp-420")
	assert.Equal(t, "Studio A", result.Maker)
}
