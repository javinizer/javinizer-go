package aventertainment

import (
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestParseDetailPageDeep2_FullPage(t *testing.T) {
	html := `<html><body>
	<span class="tag-title">ABC-123</span>
	<div class="section-title"><h1>Test Title</h1></div>
	<div class="product-info-block-rev">
		<div class="single-info"><div class="title">商品番号：</div><div class="value"><span class="tag-title">ABC-123</span></div></div>
		<div class="single-info"><div class="title">発売日：</div><div class="value">2024/03/15</div></div>
		<div class="single-info"><div class="title">収録時間：</div><div class="value">90 min</div></div>
		<div class="single-info"><div class="title">スタジオ：</div><div class="value"><a>Test Studio</a></div></div>
		<div class="single-info"><div class="title">主演女優：</div><div class="value"><a href="/ppv_actressdetail?id=1">山田花子</a></div></div>
		<div class="single-info"><div class="title">カテゴリ：</div><div class="value"><a href="?cat_id=1">Genre1</a><a href="?cat_id=2">Genre2</a></div></div>
	</div>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	result := parseDetailPage(doc, html, "https://www.aventertainments.com/ppv/detail/123", "ABC-123", "en", false)
	assert.Equal(t, "ABC-123", result.ID)
	assert.Equal(t, "Test Title", result.Title)
	assert.Equal(t, 90, result.Runtime)
	assert.Equal(t, "Test Studio", result.Maker)
	assert.NotNil(t, result.ReleaseDate)
	assert.Equal(t, []string{"Genre1", "Genre2"}, result.Genres)
	assert.Len(t, result.Actresses, 1)
	assert.Equal(t, "山田花子", result.Actresses[0].JapaneseName)
}

func TestParseDetailPageDeep2_EmptyPage(t *testing.T) {
	html := `<html><body></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	result := parseDetailPage(doc, html, "https://example.com", "FALLBACK-001", "en", false)
	assert.Equal(t, "FALLBACK-001", result.ID)
	assert.Equal(t, "FALLBACK-001", result.Title)
}

func TestParseDateDeep2_Formats(t *testing.T) {
	tests := []struct {
		input string
		year  int
		month time.Month
		day   int
		isNil bool
	}{
		{"2024-03-15", 2024, time.March, 15, false},
		{"03/15/2024", 2024, time.March, 15, false},
		{"2024/03/15", 2024, time.March, 15, false},
		{"invalid", 0, 0, 0, true},
	}
	for _, tt := range tests {
		result := parseDate(tt.input)
		if tt.isNil {
			assert.Nil(t, result, "input=%q", tt.input)
		} else {
			assert.NotNil(t, result, "input=%q", tt.input)
			assert.Equal(t, tt.year, result.Year(), "input=%q", tt.input)
			assert.Equal(t, tt.month, result.Month(), "input=%q", tt.input)
			assert.Equal(t, tt.day, result.Day(), "input=%q", tt.input)
		}
	}
}

func TestParseRuntimeDeep2_ClockFormat(t *testing.T) {
	assert.Equal(t, 90, parseRuntime("1:30:00"))
	assert.Equal(t, 60, parseRuntime("1:00:30")) // 30 seconds does not round up in this parser
}

func TestParseRuntimeDeep2_MinuteFormat(t *testing.T) {
	assert.Equal(t, 45, parseRuntime("45 min"))
	assert.Equal(t, 120, parseRuntime("120 minutes"))
	assert.Equal(t, 60, parseRuntime("60分"))
}

func TestParseRuntimeDeep2_PlainNumber(t *testing.T) {
	assert.Equal(t, 90, parseRuntime("90"))
	assert.Equal(t, 0, parseRuntime(""))
}

func TestFindDateDeep2_HTMLPatterns(t *testing.T) {
	tests := []struct {
		html     string
		expected string
	}{
		{`<span class="title">発売日</span><span class="value">2024/03/15</span>`, "2024/03/15"},
		{`2024-01-20`, "2024-01-20"},
		{`no date here`, ""},
	}
	for _, tt := range tests {
		result := findDate(tt.html)
		assert.Equal(t, tt.expected, result, "html snippet")
	}
}

func TestFindMakerDeep2_HTMLPatterns(t *testing.T) {
	html := `<span class="title">Studio</span><span class="value"><a href="/ppv/studio?id=1">Test Studio</a></span>`
	result := findMaker(html)
	assert.Equal(t, "Test Studio", result)
}

func TestFindMakerDeep2_Empty(t *testing.T) {
	assert.Equal(t, "", findMaker(""))
}

func TestIsProductIDLabelDeep2(t *testing.T) {
	assert.True(t, isProductIDLabel("商品番号"))
	assert.True(t, isProductIDLabel("品番"))
	assert.True(t, isProductIDLabel("productid"))
	assert.True(t, isProductIDLabel("item#"))
	assert.False(t, isProductIDLabel("studio"))
}

func TestIsActressLabelDeep2(t *testing.T) {
	assert.True(t, isActressLabel("主演女優"))
	assert.True(t, isActressLabel("actress"))
	assert.True(t, isActressLabel("starring"))
	assert.False(t, isActressLabel("studio"))
}

func TestIsStudioLabelDeep2(t *testing.T) {
	assert.True(t, isStudioLabel("スタジオ"))
	assert.True(t, isStudioLabel("studio"))
	assert.False(t, isStudioLabel("actress"))
}

func TestIsCategoryLabelDeep2(t *testing.T) {
	assert.True(t, isCategoryLabel("カテゴリ"))
	assert.True(t, isCategoryLabel("category"))
	assert.True(t, isCategoryLabel("categories"))
	assert.False(t, isCategoryLabel("studio"))
}

func TestIsReleaseDateLabelDeep2(t *testing.T) {
	assert.True(t, isReleaseDateLabel("発売日"))
	assert.True(t, isReleaseDateLabel("releasedate"))
	assert.True(t, isReleaseDateLabel("date"))
	assert.False(t, isReleaseDateLabel("runtime"))
}

func TestIsRuntimeLabelDeep2(t *testing.T) {
	assert.True(t, isRuntimeLabel("収録時間"))
	assert.True(t, isRuntimeLabel("runtime"))
	assert.True(t, isRuntimeLabel("playtime"))
	assert.True(t, isRuntimeLabel("length"))
	assert.False(t, isRuntimeLabel("date"))
}

func TestNormalizeInfoLabelDeep2(t *testing.T) {
	assert.Equal(t, "商品番号", normalizeInfoLabel(" 商品番号： "))
	assert.Equal(t, "productid", normalizeInfoLabel("ProductID:"))
}

func TestStripSiteSuffixDeep2(t *testing.T) {
	assert.Equal(t, "Test Movie", stripSiteSuffix("Test Movie - AV Entertainment"))
	assert.Equal(t, "Test Movie", stripSiteSuffix("Test Movie | AV ENTERTAINMENT PAY-PER-VIEW"))
	assert.Equal(t, "Regular Title", stripSiteSuffix("Regular Title"))
}

func TestExtractIDDeep2_OnePondo(t *testing.T) {
	assert.Equal(t, "1PON-020326-001", extractID("1pon_020326_001"))
}

func TestExtractIDDeep2_Caribbean(t *testing.T) {
	assert.Equal(t, "CARIB-020326-001", extractID("carib_020326_001"))
}

func TestExtractIDDeep2_Standard(t *testing.T) {
	assert.Equal(t, "ABC-123", extractID("ABC-123"))
	assert.Equal(t, "ABC-123", extractID("abc_123"))
}

func TestExtractIDDeep2_Compact(t *testing.T) {
	assert.Equal(t, "ABC123", extractID("abc123"))
}

func TestExtractIDDeep2_Empty(t *testing.T) {
	assert.Equal(t, "", extractID(""))
	assert.Equal(t, "", extractID("invalid-no-match"))
}

func TestNormalizeResolverInputDeep2_PathHandling(t *testing.T) {
	// normalizeResolverInput strips path and extension, lowercases
	result := normalizeResolverInput("/path/to/1pon_020326_001-1080p.mp4")
	assert.Equal(t, "1pon_020326_001-1080p", result)

	result = normalizeResolverInput("C:\\path\\carib-123456-001.mp4")
	assert.Contains(t, result, "carib")

	assert.Equal(t, "", normalizeResolverInput(""))
	assert.Equal(t, "", normalizeResolverInput("  "))
}

func TestIsAVEBonusScreenshotURLDeep2_Valid(t *testing.T) {
	assert.True(t, isAVEBonusScreenshotURL("/vodimages/gallery/large/ABC123/01.webp"))
	assert.True(t, isAVEBonusScreenshotURL("https://www.aventertainments.com/vodimages/gallery/large/ABC123/001.jpg"))
}

func TestIsAVEBonusScreenshotURLDeep2_Invalid(t *testing.T) {
	assert.False(t, isAVEBonusScreenshotURL(""))
	assert.False(t, isAVEBonusScreenshotURL("/vodimages/screenshot/large/ABC123.jpg"))
	assert.False(t, isAVEBonusScreenshotURL("/vodimages/gallery/large/ABC123/cover.jpg"))
}

func TestNormalizeComparableIDDeep2(t *testing.T) {
	assert.Equal(t, "abc123", normalizeComparableID("ABC-123"))
	assert.Equal(t, "abc123", normalizeComparableID("dlABC-123"))
	assert.Equal(t, "abc123", normalizeComparableID("stABC-123"))
}

func TestApplyLanguageDeep2_Japanese(t *testing.T) {
	s := &scraper{language: "ja"}
	result := s.applyLanguage("https://www.aventertainments.com/ppv/12345/1/1/new_detail")
	assert.Contains(t, result, "lang=2")
	assert.Contains(t, result, "culture=ja-JP")
}

func TestApplyLanguageDeep2_English(t *testing.T) {
	s := &scraper{language: "en"}
	result := s.applyLanguage("https://www.aventertainments.com/ppv/12345/2/1/new_detail")
	assert.Contains(t, result, "lang=1")
	assert.Contains(t, result, "culture=en-US")
}

func TestResolveSearchQueryDeep2_OnePondo(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{}}
	result, ok := s.ResolveSearchQuery("1pon_020326_001")
	assert.True(t, ok)
	assert.Equal(t, "1pon_020326_001", result)
}

func TestResolveSearchQueryDeep2_Caribbean(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{}}
	result, ok := s.ResolveSearchQuery("carib_020326_001")
	assert.True(t, ok)
	assert.Equal(t, "carib_020326_001", result)
}

func TestResolveSearchQueryDeep2_Empty(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{}}
	result, ok := s.ResolveSearchQuery("")
	assert.False(t, ok)
	assert.Equal(t, "", result)
}
