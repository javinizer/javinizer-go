package fc2

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestParseDetailPageDeep2_FullPage(t *testing.T) {
	html := `<html><head>
		<meta property="og:title" content="FC2 PPV-1234567 Test Movie | FC2">
		<meta property="og:image" content="https://example.com/cover.jpg">
		<meta property="og:description" content="Test description">
	</head><body>
		<div class="items_article_softDevice"><p>販売日：2024-03-15</p></div>
		<div class="items_article_headerInfo"><a href="/users/seller1">Seller Name</a></div>
		<div class="items_article_TagArea">
			<a class="tagTag">Tag1</a><a class="tagTag">Tag2</a>
		</div>
		<script type="application/ld+json">{"aggregateRating":{"ratingValue":4.5,"reviewCount":100}}</script>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	result := parseDetailPage(doc, html, "https://adult.contents.fc2.com/article/1234567/", "1234567")
	assert.NotNil(t, result)
	assert.Equal(t, "FC2-PPV-1234567", result.ID)
	assert.Contains(t, result.Title, "Test Movie")
	assert.Equal(t, "Test description", result.Description)
	assert.NotNil(t, result.ReleaseDate)
	assert.Equal(t, []string{"Tag1", "Tag2"}, result.Genres)
	assert.NotNil(t, result.Rating)
	assert.Equal(t, 4.5, result.Rating.Score)
	assert.Equal(t, 100, result.Rating.Votes)
}

func TestParseDetailPageDeep2_NoArticleID(t *testing.T) {
	html := `<html><body></body></html>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	result := parseDetailPage(doc, html, "", "")
	assert.Nil(t, result)
}

func TestParseRuntimeDeep2_ClockFormat(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"1:30:00", 90},
		{"0:45:30", 46}, // 30 sec rounds up
		{"45:00", 45},   // MM:SS format
		{"2:00:00", 120},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, parseRuntime(tt.input), "input=%q", tt.input)
	}
}

func TestParseRuntimeDeep2_MinuteFormat(t *testing.T) {
	assert.Equal(t, 90, parseRuntime("90 minutes"))
	assert.Equal(t, 45, parseRuntime("45min"))
	assert.Equal(t, 120, parseRuntime("120 分"))
}

func TestParseRuntimeDeep2_Empty(t *testing.T) {
	assert.Equal(t, 0, parseRuntime(""))
	assert.Equal(t, 0, parseRuntime("   "))
}

func TestParseReleaseDateDeep2_Valid(t *testing.T) {
	tm := parseReleaseDate("2024-03-15")
	assert.NotNil(t, tm)
	assert.Equal(t, 2024, tm.Year())
	assert.Equal(t, time.March, tm.Month())
	assert.Equal(t, 15, tm.Day())
}

func TestParseReleaseDateDeep2_Slashes(t *testing.T) {
	tm := parseReleaseDate("2024/03/15")
	assert.NotNil(t, tm)
	assert.Equal(t, 2024, tm.Year())
}

func TestParseReleaseDateDeep2_Invalid(t *testing.T) {
	assert.Nil(t, parseReleaseDate(""))
	assert.Nil(t, parseReleaseDate("not-a-date"))
	assert.Nil(t, parseReleaseDate("2024-13-45"))
}

func TestExtractArticleIDDeep2_Formats(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"FC2-PPV-1234567", "1234567"},
		{"fc2_ppv_1234567", "1234567"},
		{"ppv-1234567", "1234567"},
		{"1234567", "1234567"},
		{"https://adult.contents.fc2.com/article/1234567/", "1234567"},
		{"", ""},
		{"invalid", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, extractArticleID(tt.input), "input=%q", tt.input)
	}
}

func TestExtractProductIDFromHTMLDeep2(t *testing.T) {
	html := `商品ID : FC2 PPV 1234567`
	assert.Equal(t, "1234567", extractProductIDFromHTML(html))
	assert.Equal(t, "", extractProductIDFromHTML("no product ID here"))
}

func TestCanonicalFC2IDDeep2(t *testing.T) {
	assert.Equal(t, "FC2-PPV-1234567", canonicalFC2ID("1234567"))
	assert.Equal(t, "FC2-PPV-123", canonicalFC2ID(" 123"))
}

func TestStripFC2IDPrefixDeep2(t *testing.T) {
	assert.Equal(t, "Test Title", stripFC2IDPrefix("FC2-PPV-1234567 Test Title"))
	assert.Equal(t, "No Prefix", stripFC2IDPrefix("No Prefix"))
}

func TestIsFC2NotFoundPageDeep2(t *testing.T) {
	assert.True(t, isFC2NotFoundPage("お探しの商品が見つかりませんでした"))
	assert.True(t, isFC2NotFoundPage("This page may have been deleted"))
	assert.False(t, isFC2NotFoundPage("Normal page content"))
}

func TestStripSiteSuffixDeep2(t *testing.T) {
	assert.Equal(t, "Test Movie", stripSiteSuffix("Test Movie | FC2"))
	assert.Equal(t, "Test Movie", stripSiteSuffix("Test Movie｜FC2 Content"))
	assert.Equal(t, "Test Movie", stripSiteSuffix("Test Movie")) // no suffix
}

func TestNormalizeURLDeep2_ProtocolRelative(t *testing.T) {
	url := normalizeURL("//example.com/image.jpg", "https://adult.contents.fc2.com/article/123/")
	assert.Equal(t, "https://example.com/image.jpg", url)
}

func TestNormalizeURLDeep2_Absolute(t *testing.T) {
	url := normalizeURL("https://example.com/image.jpg", "https://adult.contents.fc2.com")
	assert.Equal(t, "https://example.com/image.jpg", url)
}

func TestNormalizeURLDeep2_Relative(t *testing.T) {
	url := normalizeURL("/images/pic.jpg", "https://adult.contents.fc2.com/article/123/")
	assert.Contains(t, url, "/images/pic.jpg")
}

func TestNormalizeURLDeep2_Empty(t *testing.T) {
	assert.Equal(t, "", normalizeURL("", "https://example.com"))
}

func TestExtractRatingDeep2_WithJSONLD(t *testing.T) {
	html := `<script type="application/ld+json">{"aggregateRating":{"ratingValue":4.2,"reviewCount":50}}</script>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	rating := extractRating(doc)
	assert.NotNil(t, rating)
	assert.Equal(t, 4.2, rating.Score)
	assert.Equal(t, 50, rating.Votes)
}

func TestExtractRatingDeep2_NoJSONLD(t *testing.T) {
	html := `<html><body></body></html>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	rating := extractRating(doc)
	assert.Nil(t, rating)
}

func TestExtractTagsDeep2(t *testing.T) {
	html := `<div class="items_article_TagArea">
		<a class="tagTag">Tag1</a><a class="tagTag">Tag2</a><a class="tagTag">Tag1</a>
	</div>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	tags := extractTags(doc)
	assert.Equal(t, []string{"Tag1", "Tag2"}, tags)
}

func TestExtractInfoValueDeep2(t *testing.T) {
	html := `<div class="items_article_softDevice"><p>販売日：2024-03-15</p></div>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	value := extractInfoValue(doc, "販売日")
	assert.Equal(t, "2024-03-15", value)
}

func TestToFloat64Deep2(t *testing.T) {
	assert.Equal(t, 3.14, toFloat64(3.14))
	assert.Equal(t, 42.0, toFloat64(42))
	assert.Equal(t, 7.5, toFloat64("7.5"))
	assert.Equal(t, 0.0, toFloat64("invalid"))
	assert.Equal(t, 0.0, toFloat64(nil))
}

func TestToIntDeep2(t *testing.T) {
	assert.Equal(t, 42, toInt(42))
	assert.Equal(t, 100, toInt(int64(100)))
	assert.Equal(t, 50, toInt(float64(50.7)))
	assert.Equal(t, 7, toInt("7"))
	assert.Equal(t, 0, toInt("invalid"))
	assert.Equal(t, 0, toInt(nil))
}

func TestToIntDeep2_JSONNumber(t *testing.T) {
	n := json.Number("123")
	assert.Equal(t, 123, toInt(n))
}

func TestToFloat64Deep2_JSONNumber(t *testing.T) {
	n := json.Number("3.14")
	assert.Equal(t, 3.14, toFloat64(n))
}

func TestResolveSearchQueryDeep2(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{}}
	id, ok := s.ResolveSearchQuery("FC2-PPV-1234567")
	assert.True(t, ok)
	assert.Equal(t, "FC2-PPV-1234567", id)

	id, ok = s.ResolveSearchQuery("1234567")
	assert.True(t, ok)
	assert.Equal(t, "FC2-PPV-1234567", id)

	_, ok = s.ResolveSearchQuery("invalid")
	assert.False(t, ok)
}
