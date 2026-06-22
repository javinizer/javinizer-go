package caribbeancom

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

func TestParseHTML_ExtractsMovieID(t *testing.T) {
	jsVars := `var Movie = {"movie_id":"011523-001"};`
	docHTML := `<html><head><meta property="og:title" content="Test Movie"></head></html>`
	doc := mustDoc(t, docHTML)
	result := ParseHTML(doc, jsVars, "https://www.caribbeancom.com/moviepages/011523-001/", "ja")
	assert.Equal(t, "011523-001", result.ID)
}

func TestParseHTML_ExtractsTitle(t *testing.T) {
	jsVars := `var Movie = {"movie_id":"011523-001"};`
	docHTML := `<html><head><meta property="og:title" content="Test Movie"></head></html>`
	doc := mustDoc(t, docHTML)
	result := ParseHTML(doc, jsVars, "https://www.caribbeancom.com/moviepages/011523-001/", "ja")
	assert.Equal(t, "Test Movie", result.Title)
}

func TestParseHTML_ExtractsRuntime(t *testing.T) {
	jsVars := `var Movie = {"movie_id":"011523-001"};`
	docHTML := `<html><body>
		<li class="movie-spec"><span class="spec-title">再生時間</span><span class="spec-content">90 min</span></li>
		</body></html>`
	doc := mustDoc(t, docHTML)
	result := ParseHTML(doc, jsVars, "https://www.caribbeancom.com/moviepages/011523-001/", "ja")
	assert.Equal(t, 90, result.Runtime)
}

func TestParseHTML_ExtractsReleaseDate(t *testing.T) {
	jsVars := `var Movie = {"movie_id":"011523-001"};`
	docHTML := `<html><body>
		<li class="movie-spec"><span class="spec-title">配信日</span><span class="spec-content">2023/01/15</span></li>
		</body></html>`
	doc := mustDoc(t, docHTML)
	result := ParseHTML(doc, jsVars, "https://www.caribbeancom.com/moviepages/011523-001/", "ja")
	assert.NotNil(t, result.ReleaseDate)
}

func TestParseHTML_ExtractsActresses(t *testing.T) {
	jsVars := `var Movie = {"movie_id":"011523-001"};`
	docHTML := `<html><body>
		<li class="movie-spec"><span class="spec-title">出演</span><span class="spec-content"><a itemprop="actor"><span itemprop="name">Actress1</span></a></span></li>
		</body></html>`
	doc := mustDoc(t, docHTML)
	result := ParseHTML(doc, jsVars, "https://www.caribbeancom.com/moviepages/011523-001/", "ja")
	require.Len(t, result.Actresses, 1)
	assert.Equal(t, "Actress1", result.Actresses[0].JapaneseName)
}

func TestParseHTML_ExtractsGenres(t *testing.T) {
	jsVars := `var Movie = {"movie_id":"011523-001"};`
	docHTML := `<html><body>
		<li class="movie-spec"><span class="spec-title">タグ</span><span class="spec-content"><a>Tag1</a><a>Tag2</a></span></li>
		</body></html>`
	doc := mustDoc(t, docHTML)
	result := ParseHTML(doc, jsVars, "https://www.caribbeancom.com/moviepages/011523-001/", "ja")
	assert.Equal(t, []string{"Tag1", "Tag2"}, result.Genres)
}

func TestParseHTML_FullPage(t *testing.T) {
	jsVars := `var Movie = {"movie_id":"011523-001"};`
	docHTML := `<html><head><meta property="og:title" content="Test Movie"></head>
		<body><h1 itemprop="name">Test Movie</h1>
		<p itemprop="description">A test description</p>
		<li class="movie-spec"><span class="spec-title">再生時間</span><span class="spec-content">90 min</span></li>
		<li class="movie-spec"><span class="spec-title">配信日</span><span class="spec-content">2023/01/15</span></li>
		<li class="movie-spec"><span class="spec-title">出演</span><span class="spec-content"><a itemprop="actor"><span itemprop="name">Actress1</span></a></span></li>
		<li class="movie-spec"><span class="spec-title">タグ</span><span class="spec-content"><a>Tag1</a><a>Tag2</a></span></li>
		</body></html>`
	doc := mustDoc(t, docHTML)
	result := ParseHTML(doc, jsVars, "https://www.caribbeancom.com/moviepages/011523-001/", "ja")
	assert.Equal(t, "011523-001", result.ID)
	assert.Equal(t, "Test Movie", result.Title)
	assert.Equal(t, 90, result.Runtime)
	assert.NotNil(t, result.ReleaseDate)
	assert.Len(t, result.Actresses, 1)
	assert.Len(t, result.Genres, 2)
	assert.True(t, result.ShouldCropPoster)
}
