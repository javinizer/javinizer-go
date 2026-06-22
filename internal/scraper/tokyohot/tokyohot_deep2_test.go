package tokyohot

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestParseDetailPageDeep2_FullPage(t *testing.T) {
	html := `<html><head><title>Test Movie | Tokyo Hot</title></head><body>
	<dl class="info">
		<dt>Product ID</dt><dd>n0678</dd>
		<dt>配信開始日</dt><dd>2024/03/15</dd>
		<dt>収録時間</dt><dd>01:30:00</dd>
		<dt>Maker</dt><dd><a>Test Maker</a></dd>
		<dt>Model</dt><dd>Actress1、Actress2</dd>
		<dt>Play</dt><dd>Play1、Play2</dd>
	</dl>
	<div class="sentence">Test description</div>
	<img src="https://example.com/jacket.jpg">
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	result := parseDetailPage(doc, "https://www.tokyo-hot.com/product/n0678", "n0678", "ja")
	assert.Contains(t, result.Title, "Test Movie")
	assert.Equal(t, "N0678", result.ID)
	assert.Equal(t, 90, result.Runtime)
	assert.Equal(t, "Test Maker", result.Maker)
	assert.Equal(t, "Test description", result.Description)
	assert.Len(t, result.Actresses, 2)
	assert.Len(t, result.Genres, 2)
}

func TestParseDetailPageDeep2_EmptyPage(t *testing.T) {
	html := `<html><body></body></html>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	result := parseDetailPage(doc, "", "FALLBACK-001", "ja")
	assert.Equal(t, "FALLBACK-001", result.ID)
	assert.Equal(t, "FALLBACK-001", result.Title)
}

func TestExtractIDDeep2(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"n0678", "N0678"},
		{"kb1234", "KB1234"},
		{"AB-123", "AB-123"},
		{"invalid", ""},
		{"", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, extractID(tt.input), "input=%q", tt.input)
	}
}

func TestSplitNamesDeep2(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"Actress1、Actress2", []string{"Actress1", "Actress2"}},
		{"A, B, C", []string{"A", "B", "C"}},
		{"A/B", []string{"A", "B"}},
		{"", nil},
	}
	for _, tt := range tests {
		result := splitNames(tt.input)
		assert.Equal(t, tt.expected, result, "input=%q", tt.input)
	}
}

func TestExtractInfoDDDeep2(t *testing.T) {
	html := `<dl class="info">
		<dt>Product ID</dt><dd>n0678</dd>
		<dt>配信開始日</dt><dd>2024/03/15</dd>
		<dt>収録時間</dt><dd>90 min</dd>
	</dl>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	assert.Equal(t, "n0678", extractInfoDD(doc, []string{"Product ID"}))
	assert.Equal(t, "2024/03/15", extractInfoDD(doc, []string{"配信開始日"}))
	assert.Equal(t, "90 min", extractInfoDD(doc, []string{"収録時間"}))
	assert.Equal(t, "", extractInfoDD(doc, []string{"nonexistent"}))
}

func TestExtractInfoLinkValueDeep2(t *testing.T) {
	html := `<dl class="info">
		<dt>Maker</dt><dd><a>Maker Name</a></dd>
		<dt>Series</dt><dd><a>Series Name</a></dd>
	</dl>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	assert.Equal(t, "Maker Name", extractInfoLinkValue(doc, []string{"Maker"}))
	assert.Equal(t, "Series Name", extractInfoLinkValue(doc, []string{"Series"}))
}

func TestExtractCoverURLDeep2_Jacket(t *testing.T) {
	html := `<img src="https://example.com/jacket_test.jpg">`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	url := extractCoverURL(doc, "https://www.tokyo-hot.com")
	assert.Equal(t, "https://example.com/jacket_test.jpg", url)
}

func TestExtractCoverURLDeep2_VideoPoster(t *testing.T) {
	html := `<video poster="https://example.com/poster.jpg">`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	url := extractCoverURL(doc, "https://www.tokyo-hot.com")
	assert.Equal(t, "https://example.com/poster.jpg", url)
}

func TestExtractCoverURLDeep2_OGImage(t *testing.T) {
	html := `<meta property="og:image" content="https://example.com/og_image.jpg">`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	url := extractCoverURL(doc, "https://www.tokyo-hot.com")
	assert.Equal(t, "https://example.com/og_image.jpg", url)
}

func TestExtractCoverURLDeep2_Empty(t *testing.T) {
	html := `<html><body></body></html>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	assert.Equal(t, "", extractCoverURL(doc, "https://www.tokyo-hot.com"))
}

func TestExtractScreenshotsDeep2(t *testing.T) {
	html := `<div class="scap"><a href="https://example.com/cap1.jpg"></a><a href="https://example.com/cap2.jpg"></a></div>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	urls := extractScreenshotURLs(doc, "https://www.tokyo-hot.com")
	assert.Len(t, urls, 2)
}

func TestExtractTrailerURLDeep2(t *testing.T) {
	html := `<video><source src="https://example.com/trailer.mp4"></video>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	url := extractTrailerURL(doc, "https://www.tokyo-hot.com")
	assert.Equal(t, "https://example.com/trailer.mp4", url)
}

func TestExtractTrailerURLDeep2_Empty(t *testing.T) {
	html := `<html><body></body></html>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	assert.Equal(t, "", extractTrailerURL(doc, "https://www.tokyo-hot.com"))
}

func TestExtractActressesDeep2_JapaneseNames(t *testing.T) {
	html := `<dl class="info">
		<dt>Model</dt><dd>田中麻美、佐藤美咲</dd>
	</dl>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	actresses := extractActresses(doc)
	assert.Len(t, actresses, 2)
	assert.Equal(t, "田中麻美", actresses[0].JapaneseName)
}

func TestExtractGenresDeep2(t *testing.T) {
	html := `<dl class="info">
		<dt>Play</dt><dd>Play1、Play2</dd>
	</dl>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	genres := extractGenres(doc)
	assert.Equal(t, []string{"Play1", "Play2"}, genres)
}

func TestResolveSearchQueryDeep2(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{}}
	id, ok := s.ResolveSearchQuery("n0678")
	assert.True(t, ok)
	assert.Equal(t, "N-0678", id)

	id, ok = s.ResolveSearchQuery("kb1234")
	assert.True(t, ok)
	assert.Equal(t, "KB-1234", id)

	_, ok = s.ResolveSearchQuery("invalid-format")
	assert.False(t, ok)

	_, ok = s.ResolveSearchQuery("")
	assert.False(t, ok)
}

func TestApplyLanguageDeep2(t *testing.T) {
	s := &scraper{language: "en"}
	url := s.applyLanguage("https://www.tokyo-hot.com/product/n0678")
	assert.Contains(t, url, "lang=en")

	s = &scraper{language: "ja"}
	url = s.applyLanguage("https://www.tokyo-hot.com/product/n0678")
	assert.Contains(t, url, "lang=ja")
}

func TestResolveDownloadProxyForHostDeep2(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{}}
	_, _, ok := s.ResolveDownloadProxyForHost("tokyo-hot.com")
	assert.True(t, ok)
	_, _, ok = s.ResolveDownloadProxyForHost("www.tokyo-hot.com")
	assert.True(t, ok)
	_, _, ok = s.ResolveDownloadProxyForHost("example.com")
	assert.False(t, ok)
	_, _, ok = s.ResolveDownloadProxyForHost("")
	assert.False(t, ok)
}
