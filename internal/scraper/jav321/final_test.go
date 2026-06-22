package jav321

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestParseDetailPageFinal_FullPage(t *testing.T) {
	htmlStr := `<html><body>
<div class="panel-heading"><h3>ABC-123 Movie Title</h3></div>
<b>品番</b>: <a href="/video/abc">ABC-123</a><br>
<b>発売日</b>: 2024-01-15<br>
<b>収録時間</b>: 120 minutes<br>
<b>メーカー</b>: <a href="/maker/abc">Maker X</a><br>
<b>シリーズ</b>: <a href="/series/abc">Series X</a><br>
<b>出演者</b>: <a href="/actress/abc">Actress A</a><br>
<a href="/genre/abc">Genre1</a><a href="/genre/def">Genre2</a>
<a href="/snapshot/cover"><img src="https://example.com/cover.jpg"></a>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	result := parseDetailPage(doc, "https://jp.jav321.com/video/abc-123", "ABC-123")
	if result.ID != "ABC-123" {
		t.Errorf("expected ID ABC-123, got %s", result.ID)
	}
	if result.Runtime != 120 {
		t.Errorf("expected runtime 120, got %d", result.Runtime)
	}
	if result.Maker != "Maker X" {
		t.Errorf("expected Maker X, got %s", result.Maker)
	}
	if result.Series != "Series X" {
		t.Errorf("expected Series X, got %s", result.Series)
	}
	if len(result.Genres) != 2 {
		t.Errorf("expected 2 genres, got %d", len(result.Genres))
	}
	if len(result.Actresses) != 1 {
		t.Errorf("expected 1 actress, got %d", len(result.Actresses))
	}
}

func TestParseDetailPageFinal_EmptyPage(t *testing.T) {
	htmlStr := `<html><body></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	result := parseDetailPage(doc, "https://jp.jav321.com/video/xyz-999", "XYZ-999")
	if result.ID != "XYZ-999" {
		t.Errorf("expected fallback ID XYZ-999, got %s", result.ID)
	}
	if result.Title != "XYZ-999" {
		t.Errorf("expected fallback title XYZ-999, got %s", result.Title)
	}
}

func TestExtractLabeledValueFinal(t *testing.T) {
	html := `<b>品番</b>: ABC-123<br><b>発売日</b>: 2024-01-15<br>`
	if v := extractLabeledValue(html, []string{"品番"}); v != "ABC-123" {
		t.Errorf("expected ABC-123, got %q", v)
	}
	if v := extractLabeledValue(html, []string{"発売日"}); v != "2024-01-15" {
		t.Errorf("expected 2024-01-15, got %q", v)
	}
	if v := extractLabeledValue(html, []string{"存在しない"}); v != "" {
		t.Errorf("expected empty, got %q", v)
	}
}

func TestExtractLabeledAnchorValueFinal(t *testing.T) {
	html := `<b>メーカー</b>: <a href="/maker/abc">Maker X</a><br>`
	if v := extractLabeledAnchorValue(html, []string{"メーカー"}); v != "Maker X" {
		t.Errorf("expected Maker X, got %q", v)
	}
}

func TestExtractGenresFinal(t *testing.T) {
	htmlStr := `<html><body>
<a href="/genre/abc">Genre1</a><a href="/genre/def">Genre2</a><a href="/genre/abc">Genre1</a>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	genres := extractGenres(doc)
	if len(genres) != 2 {
		t.Fatalf("expected 2 unique genres, got %d", len(genres))
	}
}

func TestExtractActressesFinal(t *testing.T) {
	html := `<b>出演者</b>: <a href="/actress/abc">Actress A</a>, <a href="/actress/def">Actress B</a><br>`
	actresses := extractActresses(html)
	if len(actresses) != 2 {
		t.Fatalf("expected 2 actresses, got %d", len(actresses))
	}
}

func TestExtractCoverURLFinal(t *testing.T) {
	htmlStr := `<html><body>
<a href="/snapshot/cover"><img src="https://example.com/cover.jpg"></a>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	url := extractCoverURL(doc, "https://jp.jav321.com")
	if url == "" {
		t.Error("expected cover URL")
	}
}

func TestExtractCoverURLFinal_MetaTag(t *testing.T) {
	htmlStr := `<html><head>
<meta property="og:image" content="https://example.com/cover.jpg">
</head></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	url := extractCoverURL(doc, "https://jp.jav321.com")
	if url == "" {
		t.Error("expected cover URL from meta tag")
	}
}

func TestExtractScreenshotURLsFinal(t *testing.T) {
	htmlStr := `<html><body>
<a href="/snapshot/1"><img src="https://example.com/ss1.jpg"></a>
<a href="/snapshot/2"><img src="https://example.com/ss2.jpg"></a>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	urls := extractScreenshotURLs(doc, "https://jp.jav321.com")
	if len(urls) != 2 {
		t.Fatalf("expected 2 screenshots, got %d", len(urls))
	}
}

func TestExtractIDFinal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"ABC-123", "ABC-123"},
		{"abc_456", ""}, // underscore not matched by regex
		{"no-match", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractID(tt.input)
		if got != tt.want {
			t.Errorf("extractID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStripTrailingIDFinal(t *testing.T) {
	result := stripTrailingID("ABC-123 Movie Title")
	if result != "Movie Title" {
		t.Errorf("expected 'Movie Title', got %q", result)
	}
}

func TestStripTrailingSiteNameFinal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"My Title - JAV321", "My Title"},
		{"My Title | JAV321", "My Title"},
		{"My Title - Jav321", "My Title"},
		{"No Suffix", "No Suffix"},
	}
	for _, tt := range tests {
		got := stripTrailingSiteName(tt.input)
		if got != tt.want {
			t.Errorf("stripTrailingSiteName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsUsableDescriptionFinal(t *testing.T) {
	if isUsableDescription("") {
		t.Error("expected empty string to not be usable")
	}
	if isUsableDescription("Short") {
		t.Error("expected short string to not be usable")
	}
	if !isUsableDescription("This is a valid description that is long enough to pass the check.") {
		t.Error("expected long string to be usable")
	}
	if isUsableDescription("adsbyjuicy some ad content that is very long and should be filtered out because it contains bad markers") {
		t.Error("expected ad content to not be usable")
	}
}

func TestSplitValuesFinal(t *testing.T) {
	result := splitValues("A, B、C/D")
	if len(result) != 4 {
		t.Fatalf("expected 4 values, got %d: %v", len(result), result)
	}
}

func TestStripTagsFinal(t *testing.T) {
	result := stripTags("<b>Hello</b> <i>World</i>")
	if result != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", result)
	}
}

func TestExtractDescriptionFinal(t *testing.T) {
	htmlStr := `<html><head>
<meta name="description" content="This is a long enough description for the movie content.">
</head></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	desc := extractDescription(doc, "")
	if desc != "This is a long enough description for the movie content." {
		t.Errorf("expected description, got %q", desc)
	}
}
