package mgstage

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestParseHTMLDeep2_FullPage(t *testing.T) {
	html := `<html><body>
	<title>「Test Movie Title」：エロ動画・アダルトビデオ -MGS動画＜プレステージ グループ＞</title>
	<table class="detail_data">
		<tr><th>品番：</th><td>GANA-2850</td></tr>
		<tr><th>配信開始日：</th><td>2024/01/15</td></tr>
		<tr><th>収録時間：</th><td>120 min</td></tr>
		<tr><th>メーカー：</th><td><a href="/maker/1">Test Maker</a></td></tr>
		<tr><th>レーベル：</th><td><a href="/label/1">Test Label</a></td></tr>
		<tr><th>シリーズ：</th><td><a href="/series/1">Test Series</a></td></tr>
		<tr><th>ジャンル：</th><td><a href="/genre/1">Genre1</a><a href="/genre/2">Genre2</a></td></tr>
		<tr><th>出演：</th><td><a href="/actress/1">田中麻美</a><a href="/actress/2">佐藤美咲</a></td></tr>
	</table>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	s := &scraper{settings: models.ScraperSettings{}}
	result, err := s.parseHTML(doc, "https://www.mgstage.com/product/product_detail/GANA-2850/")
	assert.NoError(t, err)
	assert.Equal(t, "GANA-2850", result.ID)
	assert.Equal(t, "Test Movie Title", result.Title)
	assert.Equal(t, 120, result.Runtime)
	assert.Equal(t, "Test Maker", result.Maker)
	assert.Equal(t, "Test Label", result.Label)
	assert.Equal(t, "Test Series", result.Series)
	assert.Equal(t, []string{"Genre1", "Genre2"}, result.Genres)
	assert.Len(t, result.Actresses, 2)
	assert.NotNil(t, result.ReleaseDate)
}

func TestParseHTMLDeep2_NoProductSignals(t *testing.T) {
	html := `<html><body>
	<title>エロ動画・アダルトビデオ -MGS動画＜プレステージ グループ＞</title>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	s := &scraper{settings: models.ScraperSettings{}}
	_, err = s.parseHTML(doc, "https://www.mgstage.com/product/product_detail/unknown/")
	assert.Error(t, err)
}

func TestSplitMGStageIDDeep2(t *testing.T) {
	tests := []struct {
		input     string
		expLetter string
		expNumber string
	}{
		{"GANA-2850", "GANA", "2850"},
		{"SIRO-5615", "SIRO", "5615"},
		{"LUXU-1806", "LUXU", "1806"},
		{"invalid", "", ""},
		{"123-456", "", ""}, // number prefix not letters
		{"", "", ""},
	}
	for _, tt := range tests {
		letter, number := splitMGStageID(tt.input)
		assert.Equal(t, tt.expLetter, letter, "input=%q letter", tt.input)
		assert.Equal(t, tt.expNumber, number, "input=%q number", tt.input)
	}
}

func TestExpandMGStagePrefixesDeep2(t *testing.T) {
	candidates := expandMGStagePrefixes("GANA", "2850")
	assert.NotEmpty(t, candidates)
	assert.Contains(t, candidates, "200GANA-2850")
	assert.Contains(t, candidates, "259GANA-2850")
	assert.Contains(t, candidates, "300GANA-2850")
}

func TestNormalizeMGStageIDTokenDeep2(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		ok       bool
	}{
		{"GANA-2850", "GANA-2850", true},
		{"GANA_2850", "GANA-2850", true},
		{"", "", false},
		{"invalid", "", false},
		// "gana2850" doesn't match mgstageIDPartsStrict which requires a dash/underscore
	}
	for _, tt := range tests {
		result, ok := normalizeMGStageIDToken(tt.input)
		assert.Equal(t, tt.expected, result, "input=%q", tt.input)
		assert.Equal(t, tt.ok, ok, "input=%q ok", tt.input)
	}
	// Compact format without separator doesn't match
	_, ok := normalizeMGStageIDToken("gana2850")
	assert.False(t, ok, "compact ID without separator should not match")
}

func TestMGStageIDsMatchDeep2_PrefixedID(t *testing.T) {
	assert.True(t, mgstageIDsMatch("GANA-2850", "200GANA-2850"))
	assert.True(t, mgstageIDsMatch("gana2850", "200gana2850"))
	assert.False(t, mgstageIDsMatch("GANA-2850", "OTHER-1234"))
}

func TestCleanTitleDeep2_Brackets(t *testing.T) {
	assert.Equal(t, "My Movie", cleanTitle("「My Movie」：エロ動画・アダルトビデオ -MGS動画＜プレステージ グループ＞"))
}

func TestCleanTitleDeep2_Colon(t *testing.T) {
	assert.Equal(t, "My Movie", cleanTitle("My Movie：エロ動画"))
}

func TestCleanTitleDeep2_Generic(t *testing.T) {
	assert.Equal(t, "", cleanTitle("エロ動画・アダルトビデオ -MGS動画＜プレステージ グループ＞"))
}

func TestExtractRatingDeep2_StarClass(t *testing.T) {
	html := `<div class="star_40"></div><div class="review_cnt">(10)</div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	rating := extractRating(doc)
	assert.NotNil(t, rating)
	assert.Equal(t, 8.0, rating.Score) // 40/5 = 8.0
	assert.Equal(t, 10, rating.Votes)
}

func TestExtractRatingDeep2_NoRating(t *testing.T) {
	html := `<html><body></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	rating := extractRating(doc)
	assert.Nil(t, rating)
}

func TestExtractCoverURLDeep2_EnlargeLink(t *testing.T) {
	html := `<a class="link_magnify" href="https://example.com/cover.jpg">Enlarge</a>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	cover := extractCoverURL(doc)
	assert.Equal(t, "https://example.com/cover.jpg", cover)
}

func TestExtractCoverURLDeep2_RelativePath(t *testing.T) {
	html := `<img src="/images/jacket_ps.jpg" />`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	cover := extractCoverURL(doc)
	assert.Contains(t, cover, "pl.jpg") // ps -> pl conversion
}

func TestExtractCoverURLDeep2_Empty(t *testing.T) {
	html := `<html><body></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	cover := extractCoverURL(doc)
	assert.Equal(t, "", cover)
}

func TestCreateActressInfoDeep2_Japanese(t *testing.T) {
	info := createActressInfo("田中麻美")
	assert.Equal(t, "田中麻美", info.JapaneseName)
	assert.Equal(t, "", info.FirstName)
	assert.Equal(t, "", info.LastName)
}

func TestCreateActressInfoDeep2_Western(t *testing.T) {
	info := createActressInfo("Jane Smith")
	// createActressInfo checks for Japanese characters first
	// For Western names, it splits into first/last
	assert.NotEmpty(t, info.FirstName)
}

func TestCreateActressInfoDeep2_SingleName(t *testing.T) {
	info := createActressInfo("Madoka")
	assert.Equal(t, "Madoka", info.FirstName)
	assert.Equal(t, "", info.LastName)
}

func TestHttpStatusErrorDeep2_ProxyBlocked(t *testing.T) {
	s := &scraper{usingProxy: true, settings: models.ScraperSettings{}}
	err := s.httpStatusError("detail", 403)
	assert.Contains(t, err.Error(), "proxy likely blocked")
}

func TestHttpStatusErrorDeep2_NoProxy(t *testing.T) {
	s := &scraper{usingProxy: false, settings: models.ScraperSettings{}}
	err := s.httpStatusError("detail", 403)
	assert.Contains(t, err.Error(), "access blocked")
	assert.NotContains(t, err.Error(), "proxy")
}

func TestIsGenericMGStageDescriptionDeep2(t *testing.T) {
	assert.True(t, isGenericMGStageDescription("MGS動画 エロ動画"))
	assert.False(t, isGenericMGStageDescription("This is a real description"))
	assert.False(t, isGenericMGStageDescription(""))
}
