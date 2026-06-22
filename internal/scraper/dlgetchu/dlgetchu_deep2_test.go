package dlgetchu

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
)

func TestParseDetailPageDeep2_FullPage(t *testing.T) {
	html := `<html><head><meta property="og:title" content="Test Game Title"></head><body>
	作品ID：12345
	2024/03/15
	６０分
	作品内容</td><td>This is a test description of the game content</td>
	<a href="dojin_circle_detail.php?id=1">Test Circle</a>
	<a href="genre_id=1">Genre1</a><a href="genre_id=2">Genre2</a>
	<img src="/data/item_img/abc/12345top.jpg">
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	result := parseDetailPage(doc, html, "http://dl.getchu.com/i/item12345", "12345")
	assert.Equal(t, "12345", result.ID)
	assert.Equal(t, "Test Game Title", result.Title)
	assert.Equal(t, 60, result.Runtime)
	assert.Equal(t, "Test Circle", result.Maker)
	assert.Contains(t, result.Description, "test description")
	assert.Equal(t, []string{"Genre1", "Genre2"}, result.Genres)
	assert.Contains(t, result.CoverURL, "12345top.jpg")
}

func TestParseDetailPageDeep2_EmptyPage(t *testing.T) {
	html := `<html><body></body></html>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	result := parseDetailPage(doc, html, "http://dl.getchu.com/i/item99999", "99999")
	assert.Equal(t, "99999", result.ID)
	assert.Equal(t, "99999", result.Title)
}

func TestExtractNumericIDDeep2(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"作品ID：12345", "12345"},
		{"id=67890", "67890"},
		{"/item12345", "12345"},
		{"no id here", ""},
		{"", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, extractNumericID(tt.input), "input=%q", tt.input)
	}
}

func TestNormalizeFullWidthDigitsDeep2(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"１２３", "123"},
		{"０", "0"},
		{"９９", "99"},
		{"abc", "abc"},
		{"１２３abc", "123abc"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, normalizeFullWidthDigits(tt.input), "input=%q", tt.input)
	}
}

func TestStripTagsDeep2(t *testing.T) {
	assert.Equal(t, "hello world", stripTags("<b>hello</b> <i>world</i>"))
	assert.Equal(t, "plain text", stripTags("plain text"))
	assert.Equal(t, "", stripTags(""))
}

func TestExtractGenresDeep2(t *testing.T) {
	html := `<a href="genre_id=1">Genre1</a><a href="genre_id=2">Genre2</a><a href="genre_id=1">Genre1</a>`
	genres := extractGenres(html)
	assert.Equal(t, []string{"Genre1", "Genre2"}, genres)
}

func TestExtractGenresDeep2_Empty(t *testing.T) {
	genres := extractGenres("no genres here")
	assert.Empty(t, genres)
}

func TestExtractScreenshotsDeep2(t *testing.T) {
	html := `<img src="/data/item_img/abc/01.jpg" class="highslide"><img src="/data/item_img/abc/02.jpg" class="highslide">`
	urls := extractScreenshots(html, "http://dl.getchu.com")
	assert.Len(t, urls, 2)
}

func TestExtractScreenshotsDeep2_Empty(t *testing.T) {
	urls := extractScreenshots("no screenshots", "http://dl.getchu.com")
	assert.Empty(t, urls)
}

func TestFindFirstDetailLinkDeep2_FullURL(t *testing.T) {
	html := `https://dl.getchu.com/i/item12345`
	link := findFirstDetailLink(html, "http://dl.getchu.com")
	assert.Equal(t, "https://dl.getchu.com/i/item12345", link)
}

func TestFindFirstDetailLinkDeep2_PathOnly(t *testing.T) {
	html := `<a href="/i/item12345">Link</a>`
	link := findFirstDetailLink(html, "http://dl.getchu.com")
	assert.Equal(t, "http://dl.getchu.com/i/item12345", link)
}

func TestFindFirstDetailLinkDeep2_NoLink(t *testing.T) {
	link := findFirstDetailLink("no link here", "http://dl.getchu.com")
	assert.Equal(t, "", link)
}

func TestCoverRegexDeep2(t *testing.T) {
	html := `/data/item_img/abc/12345top.jpg`
	result := parseDetailPageHTMLCoverRegex(html)
	assert.NotEmpty(t, result)
}

func parseDetailPageHTMLCoverRegex(html string) string {
	// Helper to test cover regex extraction
	m := coverRegex.FindStringSubmatch(html)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

func TestDescriptionRegexDeep2(t *testing.T) {
	html := `作品内容</td><td>This is the game description</td>`
	m := descriptionRegex.FindStringSubmatch(html)
	if len(m) > 1 {
		desc := stripTags(m[1])
		assert.Contains(t, desc, "game description")
	}
}

func TestCanHandleURLDeep2(t *testing.T) {
	s := &scraper{}
	assert.True(t, s.CanHandleURL("https://dl.getchu.com/i/item12345"))
	assert.True(t, s.CanHandleURL("http://www.getchu.com/test"))
	assert.False(t, s.CanHandleURL("https://example.com"))
}

func TestExtractIDFromURLDeep2(t *testing.T) {
	s := &scraper{}
	id, err := s.ExtractIDFromURL("https://dl.getchu.com/i/item12345")
	assert.NoError(t, err)
	assert.Equal(t, "12345", id)

	id, err = s.ExtractIDFromURL("http://dl.getchu.com/item?id=67890")
	assert.NoError(t, err)
	assert.Equal(t, "67890", id)
}
