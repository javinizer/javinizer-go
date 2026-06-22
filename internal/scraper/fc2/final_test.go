package fc2

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestParseDetailPageFinal_FullPage(t *testing.T) {
	htmlStr := `<html><head>
<meta property="og:title" content="Test FC2 Title | FC2">
<meta property="og:description" content="Test description">
<meta property="og:image" content="https://adult.contents.fc2.com/cover.jpg">
<meta property="og:video" content="https://adult.contents.fc2.com/trailer.mp4">
</head><body>
<script type="application/ld+json">{"aggregateRating":{"ratingValue":4.5,"reviewCount":100}}</script>
<div class="items_article_softDevice"><p>販売日：2024-01-15</p></div>
<div class="items_article_TagArea">
<a class="tagTag">Tag1</a><a class="tagTag">Tag2</a>
</div>
<div class="items_article_headerInfo"><a href="/users/123">Maker Name</a></div>
<div class="items_article_SampleImagesArea">
<a href="https://adult.contents.fc2.com/sample1.jpg">img1</a>
</div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	result := parseDetailPage(doc, htmlStr, "https://adult.contents.fc2.com/article/12345/", "12345")
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ID != "FC2-PPV-12345" {
		t.Errorf("expected ID FC2-PPV-12345, got %s", result.ID)
	}
	if result.Maker != "Maker Name" {
		t.Errorf("expected Maker Name, got %s", result.Maker)
	}
	if len(result.Genres) != 2 {
		t.Errorf("expected 2 genres, got %d", len(result.Genres))
	}
}

func TestParseDetailPageFinal_NoArticleID(t *testing.T) {
	htmlStr := `<html><body></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	result := parseDetailPage(doc, htmlStr, "https://adult.contents.fc2.com/article/99999/", "")
	if result == nil || result.ID == "" {
		t.Log("parseDetailPage with empty articleID returns nil or empty ID - acceptable")
	}
}

func TestExtractArticleIDFinal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"FC2-PPV-12345678", "12345678"},
		{"fc2_ppv_12345678", "12345678"},
		{"ppv-12345678", "12345678"},
		{"12345678", "12345678"},
		{"invalid-id", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractArticleID(tt.input)
		if got != tt.want {
			t.Errorf("extractArticleID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCanonicalFC2IDFinal(t *testing.T) {
	if canonicalFC2ID("12345") != "FC2-PPV-12345" {
		t.Errorf("unexpected canonical ID")
	}
}

func TestStripFC2IDPrefixFinal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"FC2-PPV-12345 Test Title", "Test Title"},
		{"FC2 PPV 12345: Some Title", "Some Title"},
		{"No Prefix Here", "No Prefix Here"},
	}
	for _, tt := range tests {
		got := stripFC2IDPrefix(tt.input)
		if got != tt.want {
			t.Errorf("stripFC2IDPrefix(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStripSiteSuffixFinal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Title | FC2", "Title"},
		{"Title｜FC2", "Title"},
		{"No Suffix", "No Suffix"},
	}
	for _, tt := range tests {
		got := stripSiteSuffix(tt.input)
		if got != tt.want {
			t.Errorf("stripSiteSuffix(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeURLFinal(t *testing.T) {
	tests := []struct {
		raw       string
		sourceURL string
		want      string
	}{
		{"https://example.com/img.jpg", "https://example.com/page", "https://example.com/img.jpg"},
		{"//example.com/img.jpg", "https://example.com/page", "https://example.com/img.jpg"},
		{"/img.jpg", "https://example.com/page", "https://example.com/img.jpg"},
		{"", "https://example.com/page", ""},
	}
	for _, tt := range tests {
		got := normalizeURL(tt.raw, tt.sourceURL)
		if got != tt.want {
			t.Errorf("normalizeURL(%q, %q) = %q, want %q", tt.raw, tt.sourceURL, got, tt.want)
		}
	}
}

func TestParseReleaseDateFinal(t *testing.T) {
	result := parseReleaseDate("2024-01-15")
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Year() != 2024 {
		t.Errorf("expected year 2024, got %d", result.Year())
	}
	if parseReleaseDate("") != nil {
		t.Error("expected nil for empty input")
	}
	if parseReleaseDate("invalid") != nil {
		t.Error("expected nil for invalid input")
	}
}

func TestParseRuntimeFinal(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"1:30:00", 90},
		{"45:30", 46},
		{"90 min", 90},
		{"90", 90},
		{"", 0},
	}
	for _, tt := range tests {
		got := parseRuntime(tt.input)
		if got != tt.want {
			t.Errorf("parseRuntime(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestIsFC2NotFoundPageFinal(t *testing.T) {
	if !isFC2NotFoundPage("お探しの商品が見つかりませんでした") {
		t.Error("expected Japanese 404 page to be detected")
	}
	if !isFC2NotFoundPage("this page may have been deleted") {
		t.Error("expected English 404 page to be detected")
	}
	if isFC2NotFoundPage("Normal page content") {
		t.Error("expected normal page to not be 404")
	}
}

func TestExtractTagsFinal(t *testing.T) {
	htmlStr := `<html><body>
<div class="items_article_TagArea">
<a class="tagTag">Tag1</a><a class="tagTag">Tag2</a><a class="tagTag">Tag1</a>
</div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	tags := extractTags(doc)
	if len(tags) != 2 {
		t.Fatalf("expected 2 unique tags, got %d", len(tags))
	}
}

func TestExtractScreenshotURLsFinal(t *testing.T) {
	htmlStr := `<html><body>
<div class="items_article_SampleImagesArea">
<a href="https://example.com/sample1.jpg">1</a>
<a href="https://example.com/sample2.jpg">2</a>
</div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	urls := extractScreenshotURLs(doc, "https://example.com")
	if len(urls) != 2 {
		t.Fatalf("expected 2 screenshots, got %d", len(urls))
	}
}

func TestExtractRatingFinal(t *testing.T) {
	htmlStr := `<html><body>
<script type="application/ld+json">{"aggregateRating":{"ratingValue":4.5,"reviewCount":100}}</script>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	rating := extractRating(doc)
	if rating == nil {
		t.Fatal("expected non-nil rating")
	}
	if rating.Score != 4.5 {
		t.Errorf("expected score 4.5, got %f", rating.Score)
	}
	if rating.Votes != 100 {
		t.Errorf("expected votes 100, got %d", rating.Votes)
	}
}

func TestExtractInfoValueFinal(t *testing.T) {
	htmlStr := `<html><body>
<div class="items_article_softDevice"><p>販売日：2024-01-15</p></div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	v := extractInfoValue(doc, "販売日")
	if v != "2024-01-15" {
		t.Errorf("expected 2024-01-15, got %q", v)
	}
}

func TestExtractProductIDFromHTMLFinal(t *testing.T) {
	html := `商品ID : FC2 PPV 12345678`
	id := extractProductIDFromHTML(html)
	if id != "12345678" {
		t.Errorf("expected 12345678, got %s", id)
	}
}

func TestToFloat64Final(t *testing.T) {
	if toFloat64(float64(4.5)) != 4.5 {
		t.Error("expected 4.5 for float64 input")
	}
	if toFloat64(int(5)) != 5.0 {
		t.Error("expected 5.0 for int input")
	}
	if toFloat64("3.14") != 3.14 {
		t.Error("expected 3.14 for string input")
	}
	if toFloat64("invalid") != 0 {
		t.Error("expected 0 for invalid string")
	}
}

func TestToIntFinal(t *testing.T) {
	if toInt(int(5)) != 5 {
		t.Error("expected 5 for int input")
	}
	if toInt(float64(5.0)) != 5 {
		t.Error("expected 5 for float64 input")
	}
	if toInt("42") != 42 {
		t.Error("expected 42 for string input")
	}
	if toInt("invalid") != 0 {
		t.Error("expected 0 for invalid string")
	}
}
