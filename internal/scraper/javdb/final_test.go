package javdb

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"golang.org/x/net/html"
)

func TestParseDetailPageFinal_MinimalHTML(t *testing.T) {
	htmlStr := `<html><body>
<div class="title is-4"><strong>ABC-123</strong> Test Movie Title</div>
<div class="movie-panel-info">
<div class="panel-block"><strong>番號：</strong><div class="value">ABC-123</div></div>
<div class="panel-block"><strong>日期：</strong><div class="value">2024-01-15</div></div>
<div class="panel-block"><strong>時長：</strong><div class="value">120分鐘</div></div>
<div class="panel-block"><strong>導演：</strong><div class="value"><a>Director Name</a></div></div>
<div class="panel-block"><strong>片商：</strong><div class="value"><a>Maker Name</a></div></div>
<div class="panel-block"><strong>發行：</strong><div class="value"><a>Label Name</a></div></div>
<div class="panel-block"><strong>系列：</strong><div class="value"><a>Series Name</a></div></div>
<div class="panel-block"><strong>評分：</strong><div class="value">4.5 (1,234)</div></div>
</div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	s := &scraper{baseURL: "https://javdb.com"}
	result, err := s.parseDetailPage(doc, "https://javdb.com/v/abc123", "ABC-123")
	if err != nil {
		t.Fatalf("parseDetailPage returned error: %v", err)
	}
	if result.ID != "ABC-123" {
		t.Errorf("expected ID ABC-123, got %s", result.ID)
	}
	if result.Runtime != 120 {
		t.Errorf("expected runtime 120, got %d", result.Runtime)
	}
	if result.Director != "Director Name" {
		t.Errorf("expected director Director Name, got %s", result.Director)
	}
	if result.Maker != "Maker Name" {
		t.Errorf("expected maker Maker Name, got %s", result.Maker)
	}
	if result.Label != "Label Name" {
		t.Errorf("expected label Label Name, got %s", result.Label)
	}
	if result.Series != "Series Name" {
		t.Errorf("expected series Series Name, got %s", result.Series)
	}
	if result.Rating == nil || result.Rating.Score != 9.0 {
		t.Errorf("expected rating score 9.0, got %v", result.Rating)
	}
	if result.ReleaseDate == nil {
		t.Error("expected release date to be set")
	}
}

func TestExtractActressesFinal_FemaleSymbolMarker(t *testing.T) {
	htmlStr := `<html><body><div class="value">
<a href="/actors/abc">Yui Hatano</a><strong class="symbol female">♀</strong>
<a href="/actors/def">Male Actor</a><strong class="symbol male">♂</strong>
</div></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	sel := doc.Find(".value")
	actresses := extractActresses(sel)
	if len(actresses) != 1 {
		t.Fatalf("expected 1 actress (female only), got %d", len(actresses))
	}
	if actresses[0].JapaneseName != "Yui Hatano" {
		t.Errorf("expected Yui Hatano, got %s", actresses[0].JapaneseName)
	}
}

func TestExtractActressesFinal_PlainText(t *testing.T) {
	htmlStr := `<html><body><div class="value">Actress A, Actress B</div></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	sel := doc.Find(".value")
	actresses := extractActresses(sel)
	if len(actresses) != 2 {
		t.Fatalf("expected 2 actresses, got %d", len(actresses))
	}
	if actresses[0].JapaneseName != "Actress A" {
		t.Errorf("expected Actress A, got %s", actresses[0].JapaneseName)
	}
	if actresses[1].JapaneseName != "Actress B" {
		t.Errorf("expected Actress B, got %s", actresses[1].JapaneseName)
	}
}

func TestScanSymbolSiblingFinal_ForwardFemale(t *testing.T) {
	node := &html.Node{
		Type: html.ElementNode,
		Data: "a",
	}
	strong := &html.Node{
		Type: html.ElementNode,
		Data: "strong",
		Attr: []html.Attribute{{Key: "class", Val: "symbol female"}},
		FirstChild: &html.Node{
			Type: html.TextNode,
			Data: "♀",
		},
	}
	node.NextSibling = strong
	result := scanSymbolSibling(node, true)
	if result != "female" {
		t.Errorf("expected 'female', got %q", result)
	}
}

func TestScanSymbolSiblingFinal_BackwardMale(t *testing.T) {
	node := &html.Node{
		Type: html.ElementNode,
		Data: "a",
	}
	strong := &html.Node{
		Type: html.ElementNode,
		Data: "strong",
		Attr: []html.Attribute{{Key: "class", Val: "symbol male"}},
		FirstChild: &html.Node{
			Type: html.TextNode,
			Data: "♂",
		},
	}
	node.PrevSibling = strong
	result := scanSymbolSibling(node, false)
	if result != "male" {
		t.Errorf("expected 'male', got %q", result)
	}
}

func TestScanSymbolSiblingFinal_StopsAtAnchor(t *testing.T) {
	node := &html.Node{
		Type: html.ElementNode,
		Data: "a",
	}
	nextAnchor := &html.Node{
		Type: html.ElementNode,
		Data: "a",
	}
	strong := &html.Node{
		Type: html.ElementNode,
		Data: "strong",
		Attr: []html.Attribute{{Key: "class", Val: "symbol female"}},
	}
	nextAnchor.NextSibling = strong
	node.NextSibling = nextAnchor
	result := scanSymbolSibling(node, true)
	if result != "" {
		t.Errorf("expected empty string (should stop at anchor), got %q", result)
	}
}

func TestExtractScreenshotURLsFinal_LinkAndImage(t *testing.T) {
	htmlStr := `<html><body>
<div class="tile-images preview-images">
<a href="https://pics.javdb.com/1.jpg">img1</a>
<a href="https://pics.javdb.com/login">login</a>
</div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	urls := extractScreenshotURLs(doc, "https://javdb.com")
	if len(urls) != 1 {
		t.Fatalf("expected 1 screenshot URL, got %d", len(urls))
	}
	if urls[0] != "https://pics.javdb.com/1.jpg" {
		t.Errorf("expected correct URL, got %s", urls[0])
	}
}

func TestExtractScreenshotURLsFinal_ImageFallback(t *testing.T) {
	htmlStr := `<html><body>
<div class="preview-images">
<img data-original="https://pics.javdb.com/s1.jpg">
<img data-original="https://pics.javdb.com/s2.jpg">
</div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	urls := extractScreenshotURLs(doc, "https://javdb.com")
	if len(urls) != 2 {
		t.Fatalf("expected 2 screenshot URLs, got %d", len(urls))
	}
}

func TestExtractScreenshotURLsFinal_SkipsVideoContainer(t *testing.T) {
	htmlStr := `<html><body>
<div class="tile-images preview-images">
<a href="https://pics.javdb.com/1.jpg" class="preview-video-container">video</a>
<a href="https://pics.javdb.com/2.jpg">img2</a>
</div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	urls := extractScreenshotURLs(doc, "https://javdb.com")
	if len(urls) != 1 {
		t.Fatalf("expected 1 screenshot URL (video container skipped), got %d", len(urls))
	}
}

func TestParseDetailPageFinal_EnglishLabels(t *testing.T) {
	htmlStr := `<html><body>
<div class="title is-4"><strong>XYZ-456</strong> English Title</div>
<div class="movie-panel-info">
<div class="panel-block"><strong>ID:</strong><div class="value">XYZ-456</div></div>
<div class="panel-block"><strong>Release Date:</strong><div class="value">2024-03-20</div></div>
<div class="panel-block"><strong>Runtime:</strong><div class="value">90 mins</div></div>
<div class="panel-block"><strong>Director:</strong><div class="value"><a>Dir Name</a></div></div>
<div class="panel-block"><strong>Maker:</strong><div class="value"><a>Studio X</a></div></div>
<div class="panel-block"><strong>Label:</strong><div class="value"><a>Label X</a></div></div>
<div class="panel-block"><strong>Series:</strong><div class="value"><a>Series X</a></div></div>
<div class="panel-block"><strong>Rating:</strong><div class="value">3.5 (500)</div></div>
<div class="panel-block"><strong>Genre:</strong><div class="value"><a>Comedy</a><a>Drama</a></div></div>
<div class="panel-block"><strong>Actress(es):</strong><div class="value"><a>Jane Doe</a></div></div>
</div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	s := &scraper{baseURL: "https://javdb.com"}
	result, err := s.parseDetailPage(doc, "https://javdb.com/v/xyz456", "XYZ-456")
	if err != nil {
		t.Fatalf("parseDetailPage returned error: %v", err)
	}
	if result.ID != "XYZ-456" {
		t.Errorf("expected ID XYZ-456, got %s", result.ID)
	}
	if result.Runtime != 90 {
		t.Errorf("expected runtime 90, got %d", result.Runtime)
	}
	if result.Director != "Dir Name" {
		t.Errorf("expected director Dir Name, got %s", result.Director)
	}
	if len(result.Actresses) != 1 || result.Actresses[0].JapaneseName != "Jane Doe" {
		t.Errorf("expected 1 actress Jane Doe, got %v", result.Actresses)
	}
	if len(result.Genres) != 2 {
		t.Errorf("expected 2 genres, got %d", len(result.Genres))
	}
}

func TestParseDetailPageFinal_CoverAndTrailer(t *testing.T) {
	htmlStr := `<html><body>
<div class="title is-4"><strong>AAA-001</strong> Title</div>
<div class="column-video-cover"><img class="video-cover" src="https://pics.javdb.com/cover.jpg"></div>
<video id="preview-video"><source src="https://pics.javdb.com/trailer.mp4"></video>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	s := &scraper{baseURL: "https://javdb.com"}
	result, err := s.parseDetailPage(doc, "https://javdb.com/v/aaa001", "AAA-001")
	if err != nil {
		t.Fatalf("parseDetailPage returned error: %v", err)
	}
	if result.CoverURL != "https://pics.javdb.com/cover.jpg" {
		t.Errorf("expected cover URL, got %s", result.CoverURL)
	}
	if result.TrailerURL != "https://pics.javdb.com/trailer.mp4" {
		t.Errorf("expected trailer URL, got %s", result.TrailerURL)
	}
}

func TestParseDetailPageFinal_MaleActorRow(t *testing.T) {
	htmlStr := `<html><body>
<div class="title is-4"><strong>BBB-002</strong> Title</div>
<div class="movie-panel-info">
<div class="panel-block"><strong>Male actor(s):</strong><div class="value"><a>John Male</a></div></div>
<div class="panel-block"><strong>Actress(es):</strong><div class="value"><a>Jane Female</a></div></div>
</div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	s := &scraper{baseURL: "https://javdb.com"}
	result, err := s.parseDetailPage(doc, "https://javdb.com/v/bbb002", "BBB-002")
	if err != nil {
		t.Fatalf("parseDetailPage returned error: %v", err)
	}
	if len(result.Actresses) != 1 {
		t.Fatalf("expected 1 actress, got %d", len(result.Actresses))
	}
	if result.Actresses[0].JapaneseName != "Jane Female" {
		t.Errorf("expected Jane Female, got %s", result.Actresses[0].JapaneseName)
	}
}

func TestParseDetailPageFinal_GenericCastFallback(t *testing.T) {
	htmlStr := `<html><body>
<div class="title is-4"><strong>CCC-003</strong> Title</div>
<div class="movie-panel-info">
<div class="panel-block"><strong>Cast:</strong><div class="value"><a>Some Actress</a></div></div>
</div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	s := &scraper{baseURL: "https://javdb.com"}
	result, err := s.parseDetailPage(doc, "https://javdb.com/v/ccc003", "CCC-003")
	if err != nil {
		t.Fatalf("parseDetailPage returned error: %v", err)
	}
	if len(result.Actresses) != 1 {
		t.Fatalf("expected 1 actress, got %d", len(result.Actresses))
	}
	if result.Actresses[0].JapaneseName != "Some Actress" {
		t.Errorf("expected Some Actress, got %s", result.Actresses[0].JapaneseName)
	}
}

func TestParseRatingFinal_VariousFormats(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantNil bool
		score   float64
	}{
		{"empty", "", true, 0},
		{"score_only", "4.2", false, 8.4},
		{"score_with_votes", "3.5 (1,234)", false, 7.0},
		{"zero", "0.0", true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := parseRating(tt.input)
			if tt.wantNil {
				if r != nil {
					t.Errorf("expected nil rating, got %v", r)
				}
				return
			}
			if r == nil {
				t.Fatalf("expected non-nil rating")
			}
			if r.Score != tt.score {
				t.Errorf("expected score %f, got %f", tt.score, r.Score)
			}
		})
	}
}

func TestHasDetailMetadataFinal(t *testing.T) {
	tests := []struct {
		name       string
		result     *models.ScraperResult
		fallbackID string
		want       bool
	}{
		{"nil_result", nil, "ABC-123", false},
		{"cover_url", &models.ScraperResult{CoverURL: "http://example.com/cover.jpg"}, "", true},
		{"runtime", &models.ScraperResult{Runtime: 120}, "", true},
		{"actresses", &models.ScraperResult{Actresses: []models.ActressInfo{{JapaneseName: "Test"}}}, "", true},
		{"genres", &models.ScraperResult{Genres: []string{"Drama"}}, "", true},
		{"screenshots", &models.ScraperResult{ScreenshotURL: []string{"http://example.com/ss.jpg"}}, "", true},
		{"title_different_from_id", &models.ScraperResult{Title: "Some Movie"}, "ABC-123", true},
		{"title_same_as_id", &models.ScraperResult{Title: "ABC-123"}, "ABC-123", false},
		{"empty_result", &models.ScraperResult{}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasDetailMetadata(tt.result, tt.fallbackID)
			if got != tt.want {
				t.Errorf("hasDetailMetadata() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractFirstURLFinal(t *testing.T) {
	htmlStr := `<html><body>
<img class="video-cover" data-original="https://pics.example.com/cover.jpg">
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	result := extractFirstURL(doc, []string{"img.video-cover"}, "https://example.com")
	if result != "https://pics.example.com/cover.jpg" {
		t.Errorf("expected cover URL, got %s", result)
	}
}

func TestExtractFirstURLFinal_EmptyDocument(t *testing.T) {
	htmlStr := `<html><body></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	result := extractFirstURL(doc, []string{"img.video-cover"}, "https://example.com")
	if result != "" {
		t.Errorf("expected empty URL, got %s", result)
	}
}

func TestNodeAttrFinal(t *testing.T) {
	n := &html.Node{
		Type: html.ElementNode,
		Data: "div",
		Attr: []html.Attribute{
			{Key: "class", Val: "test-class"},
			{Key: "id", Val: "test-id"},
		},
	}
	if v := nodeAttr(n, "class"); v != "test-class" {
		t.Errorf("expected test-class, got %s", v)
	}
	if v := nodeAttr(n, "nonexistent"); v != "" {
		t.Errorf("expected empty, got %s", v)
	}
}

func TestNodeTextFinal(t *testing.T) {
	parent := &html.Node{
		Type: html.ElementNode,
		Data: "strong",
	}
	child := &html.Node{
		Type: html.TextNode,
		Data: "Hello World",
	}
	parent.FirstChild = child
	if v := nodeText(parent); v != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", v)
	}
}

func TestNodeTextFinal_Nil(t *testing.T) {
	if v := nodeText(nil); v != "" {
		t.Errorf("expected empty string for nil, got %q", v)
	}
}

func TestIsLikelyMaleActorLinkFinal_ClassMarker(t *testing.T) {
	htmlStr := `<a class="gender-male" data-gender="male">Male Actor</a>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	a := doc.Find("a").First()
	if !isLikelyMaleActorLink(a) {
		t.Error("expected male actor link to be detected")
	}
}

func TestIsLikelyMaleActorLinkFinal_FemaleNotDetected(t *testing.T) {
	// A link with only female markers should not be detected as male
	htmlStr := `<a class="actress-link">Female Actress</a>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	a := doc.Find("a").First()
	if isLikelyMaleActorLink(a) {
		t.Error("female actress link should not be detected as male")
	}
}

func TestGenderHintFromSymbolSiblingFinal_Nil(t *testing.T) {
	result := genderHintFromSymbolSibling(nil)
	if result != "" {
		t.Errorf("expected empty for nil, got %q", result)
	}
}

func TestExtractStringListFinal_CommaSeparated(t *testing.T) {
	htmlStr := `<div>Tag A, Tag B, Tag C</div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	result := extractStringList(doc.Find("div"))
	if len(result) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result))
	}
}

func TestExtractStringListFinal_NAValues(t *testing.T) {
	htmlStr := `<div>n/a</div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	result := extractStringList(doc.Find("div"))
	if result != nil {
		t.Errorf("expected nil for N/A value, got %v", result)
	}
}

func TestExtractFirstTextFinal(t *testing.T) {
	htmlStr := `<div><a>Link Text</a>Plain Text</div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	result := extractFirstText(doc.Find("div"))
	if result != "Link Text" {
		t.Errorf("expected 'Link Text', got %q", result)
	}
}

func TestExtractFirstTextFinal_NoLink(t *testing.T) {
	htmlStr := `<div>Plain Text Only</div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	result := extractFirstText(doc.Find("div"))
	if result != "Plain Text Only" {
		t.Errorf("expected 'Plain Text Only', got %q", result)
	}
}

func TestExtractTrailerURLFinal(t *testing.T) {
	htmlStr := `<html><body>
<video id="preview-video"><source src="https://example.com/trailer.mp4"></video>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	result := extractTrailerURL(doc, "https://example.com")
	if result != "https://example.com/trailer.mp4" {
		t.Errorf("expected trailer URL, got %s", result)
	}
}

func TestExtractTrailerURLFinal_Empty(t *testing.T) {
	htmlStr := `<html><body></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	result := extractTrailerURL(doc, "https://example.com")
	if result != "" {
		t.Errorf("expected empty trailer URL, got %s", result)
	}
}

func TestParseDetailPageFinal_WithDescription(t *testing.T) {
	htmlStr := `<html><body>
<div class="title is-4"><strong>DDD-004</strong> Title</div>
<span itemprop="description">This is a description of the movie.</span>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	s := &scraper{baseURL: "https://javdb.com"}
	result, err := s.parseDetailPage(doc, "https://javdb.com/v/ddd004", "DDD-004")
	if err != nil {
		t.Fatalf("parseDetailPage returned error: %v", err)
	}
	if result.Description != "This is a description of the movie." {
		t.Errorf("expected description, got %q", result.Description)
	}
}

func TestParseDetailPageFinal_FallbackID(t *testing.T) {
	htmlStr := `<html><body></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	s := &scraper{baseURL: "https://javdb.com"}
	result, err := s.parseDetailPage(doc, "https://javdb.com/v/eee005", "EEE-005")
	if err != nil {
		t.Fatalf("parseDetailPage returned error: %v", err)
	}
	if result.ID != "EEE-005" {
		t.Errorf("expected fallback ID EEE-005, got %s", result.ID)
	}
	if result.Title != "EEE-005" {
		t.Errorf("expected title to be fallback ID, got %s", result.Title)
	}
}
