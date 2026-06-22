package javdb

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestParseDetailPageDeep2_FullHTML(t *testing.T) {
	html := `<html><body>
	<div class="title is-4"><strong>ABC-123</strong> Test Movie Title</div>
	<div class="movie-panel-info">
		<div class="panel-block"><strong>番號：</strong><div class="value">ABC-123</div></div>
		<div class="panel-block"><strong>發行日期：</strong><div class="value">2024-01-15</div></div>
		<div class="panel-block"><strong>時長：</strong><div class="value">120 min</div></div>
		<div class="panel-block"><strong>導演：</strong><div class="value"><a>Test Director</a></div></div>
		<div class="panel-block"><strong>片商：</strong><div class="value"><a>Test Maker</a></div></div>
		<div class="panel-block"><strong>發行：</strong><div class="value"><a>Test Label</a></div></div>
		<div class="panel-block"><strong>系列：</strong><div class="value"><a>Test Series</a></div></div>
		<div class="panel-block"><strong>評分：</strong><div class="value">4.5 (100 votes)</div></div>
		<div class="panel-block"><strong>類別：</strong><div class="value"><a>Genre1</a><a>Genre2</a></div></div>
	</div>
	</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	s := &scraper{baseURL: "https://javdb.com"}
	result, err := s.parseDetailPage(doc, "https://javdb.com/v/abc123", "ABC-123")
	assert.NoError(t, err)
	assert.Equal(t, "ABC-123", result.ID)
	assert.Contains(t, result.Title, "Test Movie Title")
	assert.Equal(t, 120, result.Runtime)
	assert.Equal(t, "Test Director", result.Director)
	assert.Equal(t, "Test Maker", result.Maker)
	assert.Equal(t, "Test Label", result.Label)
	assert.Equal(t, "Test Series", result.Series)
	assert.NotNil(t, result.ReleaseDate)
	assert.NotNil(t, result.Rating)
	assert.Equal(t, 9.0, result.Rating.Score) // 4.5 * 2
	assert.Equal(t, 100, result.Rating.Votes)
	assert.Equal(t, []string{"Genre1", "Genre2"}, result.Genres)
}

func TestParseDetailPageDeep2_EmptyPage(t *testing.T) {
	html := `<html><body></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	s := &scraper{baseURL: "https://javdb.com"}
	result, err := s.parseDetailPage(doc, "https://javdb.com/v/abc123", "fallback-id")
	assert.NoError(t, err)
	assert.Equal(t, "fallback-id", result.ID)
	assert.Equal(t, "fallback-id", result.Title)
}

func TestScanSymbolSiblingDeep2(t *testing.T) {
	html := `<span class="symbol female">♀</span><a href="/actor/1">Actress</a>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	anchor := doc.Find("a").First()
	assert.Equal(t, 1, anchor.Length())
	if anchor.Length() > 0 && len(anchor.Nodes) > 0 {
		// scanSymbolSibling looks for <strong class="symbol"> but we have <span class="symbol female">
		// The function only matches "strong" elements, so this returns "" with <span>
		hint := scanSymbolSibling(anchor.Nodes[0], true)
		assert.Equal(t, "", hint) // <span> is not <strong>
	}
}

func TestGenderHintFromSymbolSiblingDeep2_NilSelection(t *testing.T) {
	hint := genderHintFromSymbolSibling(nil)
	assert.Equal(t, "", hint)
}

func TestNodeAttrDeep2(t *testing.T) {
	html := `<div class="test-class" id="test-id">content</div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	div := doc.Find("div").First()
	assert.Equal(t, 1, div.Length())
	if div.Length() > 0 && len(div.Nodes) > 0 {
		assert.Equal(t, "test-class", nodeAttr(div.Nodes[0], "class"))
		assert.Equal(t, "test-id", nodeAttr(div.Nodes[0], "id"))
		assert.Equal(t, "", nodeAttr(div.Nodes[0], "nonexistent"))
	}
}

func TestNodeTextDeep2_NilNode(t *testing.T) {
	result := nodeText(nil)
	assert.Equal(t, "", result)
}

func TestNodeTextDeep2_NestedContent(t *testing.T) {
	html := `<div>hello <span>world</span></div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	div := doc.Find("div").First()
	assert.Equal(t, 1, div.Length())
	if div.Length() > 0 && len(div.Nodes) > 0 {
		text := nodeText(div.Nodes[0])
		assert.Contains(t, text, "hello")
		assert.Contains(t, text, "world")
	}
}

func TestParseRatingDeep2_NoVotes(t *testing.T) {
	r := parseRating("3.5")
	assert.NotNil(t, r)
	assert.Equal(t, 7.0, r.Score) // 3.5 * 2
	// When there are no explicit votes, the rating regex may still match something
	// The exact Votes value depends on regex behavior
}

func TestParseRatingDeep2_EmptyString(t *testing.T) {
	r := parseRating("")
	assert.Nil(t, r)
}

func TestParseRatingDeep2_WithVotes(t *testing.T) {
	r := parseRating("4.2 (1,234 votes)")
	assert.NotNil(t, r)
	assert.Equal(t, 8.4, r.Score)
	assert.Equal(t, 1234, r.Votes)
}

func TestParseRatingDeep2_ScoreAboveFive(t *testing.T) {
	// Scores above 5 should NOT be multiplied by 2
	r := parseRating("8.5 (50 votes)")
	assert.NotNil(t, r)
	assert.Equal(t, 8.5, r.Score)
	assert.Equal(t, 50, r.Votes)
}

func TestIsJavDBVideoCodeDeep2(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"AbJEe", true},
		{"5aB3d", true},
		{"abc", true},
		{"a", false},             // too short
		{"abcdef1234567", false}, // too long
		{"AB-123", false},        // has dash
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, isJavDBVideoCode(tt.input), "input=%q", tt.input)
	}
}

func TestTrimNumericPaddingDeep2(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ABC001", "ABC1"},
		{"ABC000", "ABC0"},
		{"ABC100", "ABC100"},
		{"AB123CD", "AB123CD"},
		{"NODIGIT", "NODIGIT"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, trimNumericPadding(tt.input), "input=%q", tt.input)
	}
}

func TestIdMatchRankDeep2_EdgeCases(t *testing.T) {
	assert.Equal(t, idMatchNone, idMatchRank("", "ABC123"))
	assert.Equal(t, idMatchNone, idMatchRank("ABC123", ""))
	assert.Equal(t, idMatchExact, idMatchRank("ABC-123", "ABC123"))
	assert.Equal(t, idMatchNormalized, idMatchRank("ABC-001", "ABC1"))
	assert.Equal(t, idMatchVariant, idMatchRank("ABC123A", "ABC123"))
}

func TestExtractScreenshotURLsDeep2_LoginSkip(t *testing.T) {
	html := `<div class="tile-images preview-images">
		<a href="/login?return=/v/abc123">Login to view</a>
		<a href="https://example.com/pic1.jpg"><img src="https://example.com/pic1.jpg"/></a>
	</div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	urls := extractScreenshotURLs(doc, "https://javdb.com")
	// Should skip /login links
	for _, u := range urls {
		assert.NotContains(t, u, "/login")
	}
}

func TestExtractTrailerURLDeep2_WithVideo(t *testing.T) {
	html := `<video id="preview-video"><source src="https://example.com/trailer.mp4"></video>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	url := extractTrailerURL(doc, "https://javdb.com")
	assert.Equal(t, "https://example.com/trailer.mp4", url)
}

func TestIsLikelyMaleActorLinkDeep2_ClassAttribute(t *testing.T) {
	html := `<a class="gender-male" href="/actor/1">Male Actor</a>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	a := doc.Find("a").First()
	assert.True(t, isLikelyMaleActorLink(a))
}

func TestIsLikelyMaleActorLinkDeep2_DataGender(t *testing.T) {
	html := `<a data-gender="male" href="/actor/1">Actor</a>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	assert.NoError(t, err)

	a := doc.Find("a").First()
	assert.True(t, isLikelyMaleActorLink(a))
}

func TestHasDetailMetadataDeep2_NilResult(t *testing.T) {
	assert.False(t, hasDetailMetadata(nil, "ABC-123"))
}

func TestHasDetailMetadataDeep2_TitleMatchesFallbackID(t *testing.T) {
	result := &models.ScraperResult{Title: "ABC-123"}
	assert.False(t, hasDetailMetadata(result, "ABC-123"))
}

func TestHasDetailMetadataDeep2_CoverURL(t *testing.T) {
	result := &models.ScraperResult{CoverURL: "https://example.com/cover.jpg"}
	assert.True(t, hasDetailMetadata(result, ""))
}

func TestHasDetailMetadataDeep2_Runtime(t *testing.T) {
	result := &models.ScraperResult{Runtime: 120}
	assert.True(t, hasDetailMetadata(result, ""))
}
