package jav321

import (
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
)

func TestParseDetailPageDeep2_FullPage(t *testing.T) {
	html := `<html><body>
	<div class="panel-heading"><h3>ABC-123 Test Movie Title</h3></div>
	<b>品番</b>: ABC-123<br>
	<b>発売日</b>: 2024-03-15<br>
	<b>収録時間</b>: 120分<br>
	<b>メーカー</b>: <a>Test Maker</a><br>
	<b>シリーズ</b>: <a>Test Series</a><br>
	<b>出演者</b>: <a>田中麻美</a><a>佐藤美咲</a><br>
	<meta name="description" content="This is a valid movie description that is long enough to pass the filter check">
	<a href="/genre/1">Genre1</a><a href="/genre/2">Genre2</a>
	<a href="/snapshot/"><img src="https://example.com/cover.jpg"></a>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	result := parseDetailPage(doc, "https://jp.jav321.com/video/ABC-123", "ABC-123")
	assert.Equal(t, "ABC-123", result.ID)
	assert.Equal(t, 120, result.Runtime)
	assert.Equal(t, "Test Maker", result.Maker)
	assert.Equal(t, "Test Series", result.Series)
	assert.NotNil(t, result.ReleaseDate)
	assert.Equal(t, time.March, result.ReleaseDate.Month())
	assert.Len(t, result.Actresses, 2)
	assert.Equal(t, "田中麻美", result.Actresses[0].JapaneseName)
}

func TestParseDetailPageDeep2_EmptyPage(t *testing.T) {
	html := `<html><body></body></html>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	result := parseDetailPage(doc, "https://jp.jav321.com/video/FALLBACK-001", "FALLBACK-001")
	assert.Equal(t, "FALLBACK-001", result.ID)
	assert.Equal(t, "FALLBACK-001", result.Title)
}

func TestExtractLabeledValueDeep2_MultipleLabels(t *testing.T) {
	html := `<b>品番</b>: IPX-456<br><b>発売日</b>: 2024-01-01<br>`
	assert.Equal(t, "IPX-456", extractLabeledValue(html, []string{"品番"}))
	assert.Equal(t, "2024-01-01", extractLabeledValue(html, []string{"発売日"}))
	assert.Equal(t, "", extractLabeledValue(html, []string{"nonexistent"}))
}

func TestExtractLabeledAnchorValueDeep2(t *testing.T) {
	html := `<b>メーカー</b>: <a href="/maker/1">Maker Name</a><br>`
	assert.Equal(t, "Maker Name", extractLabeledAnchorValue(html, []string{"メーカー"}))
}

func TestIsUsableDescriptionDeep2(t *testing.T) {
	assert.True(t, isUsableDescription("This is a valid description that is long enough to be useful"))
	assert.False(t, isUsableDescription(""))          // empty
	assert.False(t, isUsableDescription("too short")) // too short
	assert.False(t, isUsableDescription("adsbyjuicy some ad content that is long enough to pass the length check"))
	assert.False(t, isUsableDescription("window.ads and some other content that is long enough to pass the length check"))
}

func TestSplitValuesDeep2(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"A、B、C", []string{"A", "B", "C"}},
		{"A, B, C", []string{"A", "B", "C"}},
		{"A/B/C", []string{"A", "B", "C"}},
		{"A|B|C", []string{"A", "B", "C"}},
	}
	for _, tt := range tests {
		result := splitValues(tt.input)
		assert.Equal(t, tt.expected, result, "input=%q", tt.input)
	}
	// Empty returns empty slice, not nil
	assert.Empty(t, splitValues(""))
}

func TestExtractIDDeep2_Formats(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ABC-123", "ABC-123"},
		{"ABC123", "ABC123"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, extractID(tt.input), "input=%q", tt.input)
	}
	// abc_123 doesn't match the ID regex (requires - or no separator, not _)
	assert.Equal(t, "", extractID("abc_123"))
	assert.Equal(t, "", extractID("invalid"))
	assert.Equal(t, "", extractID(""))
}

func TestStripTrailingIDDeep2(t *testing.T) {
	assert.Equal(t, "Test Movie", stripTrailingID("Test Movie ABC-123"))
	assert.Equal(t, "Test Movie", stripTrailingID("Test Movie"))
}

func TestStripTrailingSiteNameDeep2(t *testing.T) {
	assert.Equal(t, "Test Movie", stripTrailingSiteName("Test Movie - JAV321"))
	assert.Equal(t, "Test Movie", stripTrailingSiteName("Test Movie | JAV321"))
	assert.Equal(t, "Test Movie", stripTrailingSiteName("Test Movie - Jav321"))
	assert.Equal(t, "Test Movie", stripTrailingSiteName("Test Movie"))
}

func TestStripTagsDeep2(t *testing.T) {
	assert.Equal(t, "hello world", stripTags("<b>hello</b> <i>world</i>"))
	assert.Equal(t, "plain text", stripTags("plain text"))
	assert.Equal(t, "", stripTags(""))
}

func TestExtractGenresDeep2(t *testing.T) {
	html := `<a href="/genre/1">Genre1</a><a href="/genre/2">Genre2</a><a href="/genre/1">Genre1</a>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	genres := extractGenres(doc)
	assert.Equal(t, []string{"Genre1", "Genre2"}, genres)
}

func TestExtractActressesDeep2(t *testing.T) {
	html := `<b>出演者</b>: <a>田中麻美</a><a>佐藤美咲</a><br>`
	actresses := extractActresses(html)
	assert.Len(t, actresses, 2)
	assert.Equal(t, "田中麻美", actresses[0].JapaneseName)
	assert.Equal(t, "佐藤美咲", actresses[1].JapaneseName)
}

func TestExtractActressesDeep2_FallbackValues(t *testing.T) {
	html := `<b>出演者</b>: 田中麻美、佐藤美咲<br>`
	actresses := extractActresses(html)
	assert.Len(t, actresses, 2)
}

func TestExtractCoverURLDeep2_OGImage(t *testing.T) {
	html := `<html><head><meta property="og:image" content="https://example.com/cover.jpg"></head></html>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	url := extractCoverURL(doc, "https://jp.jav321.com")
	assert.Equal(t, "https://example.com/cover.jpg", url)
}

func TestExtractCoverURLDeep2_Empty(t *testing.T) {
	html := `<html><body></body></html>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	assert.Equal(t, "", extractCoverURL(doc, "https://jp.jav321.com"))
}

func TestExtractScreenshotURLsDeep2(t *testing.T) {
	html := `<a href="/snapshot/1"><img src="https://example.com/snap1.jpg"></a><a href="/snapshot/2"><img src="https://example.com/snap2.jpg"></a>`
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))

	urls := extractScreenshotURLs(doc, "https://jp.jav321.com")
	assert.Len(t, urls, 2)
}

func TestExtractLabeledAnchorValuesDeep2(t *testing.T) {
	html := `<b>出演者</b>: <a>Actress A</a><a>Actress B</a><br>`
	values := extractLabeledAnchorValues(html, []string{"出演者"})
	assert.Equal(t, []string{"Actress A", "Actress B"}, values)
}
