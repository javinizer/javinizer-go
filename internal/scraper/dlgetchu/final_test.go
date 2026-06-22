package dlgetchu

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestParseDetailPageFinal_FullPage(t *testing.T) {
	htmlStr := `<html><head>
<meta property="og:title" content="Test DLgetchu Title">
</head><body>
作品ID：12345
2024/01/15
６０分
<a href="dojin_circle_detail.php?id=1">Maker X</a>
<a href="genre_id=1">Genre1</a><a href="genre_id=2">Genre2</a>
<img src="/data/item_img/abc/12345top.jpg">
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	result := parseDetailPage(doc, htmlStr, "http://dl.getchu.com/i/item12345", "12345")
	if result.ID != "12345" {
		t.Errorf("expected ID 12345, got %s", result.ID)
	}
	if result.Title != "Test DLgetchu Title" {
		t.Errorf("expected title, got %s", result.Title)
	}
	if result.Runtime != 60 {
		t.Errorf("expected runtime 60, got %d", result.Runtime)
	}
	if result.Maker != "Maker X" {
		t.Errorf("expected Maker X, got %s", result.Maker)
	}
	if len(result.Genres) != 2 {
		t.Errorf("expected 2 genres, got %d", len(result.Genres))
	}
}

func TestExtractNumericIDFinal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"作品ID：12345", "12345"},
		{"id=67890", "67890"},
		{"/item1234", "1234"},
		{"no id here", ""},
	}
	for _, tt := range tests {
		got := extractNumericID(tt.input)
		if got != tt.want {
			t.Errorf("extractNumericID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeFullWidthDigitsFinal(t *testing.T) {
	result := normalizeFullWidthDigits("１２３")
	if result != "123" {
		t.Errorf("expected 123, got %q", result)
	}
}

func TestExtractGenresFinal(t *testing.T) {
	html := `<a href="genre_id=1">Comedy</a><a href="genre_id=2">Drama</a>`
	genres := extractGenres(html)
	if len(genres) != 2 {
		t.Fatalf("expected 2 genres, got %d", len(genres))
	}
}

func TestExtractScreenshotsFinal(t *testing.T) {
	html := `<a href="/data/item_img/abc/01.jpg" class="highslide">1</a>`
	urls := extractScreenshots(html, "http://dl.getchu.com")
	if len(urls) != 1 {
		t.Fatalf("expected 1 screenshot, got %d", len(urls))
	}
}

func TestFindFirstDetailLinkFinal(t *testing.T) {
	tests := []struct {
		html  string
		base  string
		found bool
	}{
		{"https://dl.getchu.com/i/item12345", "http://dl.getchu.com", true},
		{"/i/item12345", "http://dl.getchu.com", true},
		{"no link here", "http://dl.getchu.com", false},
	}
	for _, tt := range tests {
		got := findFirstDetailLink(tt.html, tt.base)
		if (got != "") != tt.found {
			t.Errorf("findFirstDetailLink() found=%v, want %v", got != "", tt.found)
		}
	}
}

func TestParseDetailPageFinal_EmptyPage(t *testing.T) {
	htmlStr := `<html><body></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	result := parseDetailPage(doc, htmlStr, "http://dl.getchu.com/i/item99999", "99999")
	if result.ID != "99999" {
		t.Errorf("expected fallback ID 99999, got %s", result.ID)
	}
	if result.Title != "99999" {
		t.Errorf("expected fallback title 99999, got %s", result.Title)
	}
}

func TestParseDetailPageFinal_WithCover(t *testing.T) {
	htmlStr := `<html><body>
<meta property="og:title" content="Cover Test">
<img src="/data/item_img/abc/12345top.jpg">
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	result := parseDetailPage(doc, htmlStr, "http://dl.getchu.com/i/item12345", "12345")
	if result.CoverURL == "" {
		t.Error("expected cover URL")
	}
}
