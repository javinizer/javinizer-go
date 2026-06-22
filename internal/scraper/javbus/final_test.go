package javbus

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
)

func TestParseDetailPageFinal_FullPage(t *testing.T) {
	htmlStr := `<html><head><title>ABC-123 Movie Title - JavBus</title></head><body>
<div id="info">
<p><span class="header">品番:</span> ABC-123</p>
<p><span class="header">発売日:</span> 2024-01-15</p>
<p><span class="header">収録時間:</span> 120分鐘</p>
<p><span class="header">監督:</span> <a>Director X</a></p>
<p><span class="header">メーカー:</span> <a>Maker X</a></p>
<p><span class="header">レーベル:</span> <a>Label X</a></p>
<p><span class="header">シリーズ:</span> <a>Series X</a></p>
</div>
<div id="genre-toggle"><a>Genre1</a><a>Genre2</a></div>
<div id="star-div"><a href="/star/abc"><img title="Actress A" src="thumb1.jpg"></a></div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	s := &scraper{language: "ja", client: resty.New()}
	result, err := s.parseDetailPage(doc, "https://www.javbus.com/ABC-123", "ABC-123")
	if err != nil {
		t.Fatalf("parseDetailPage returned error: %v", err)
	}
	if result.ID != "ABC-123" {
		t.Errorf("expected ID ABC-123, got %s", result.ID)
	}
	if result.Runtime != 120 {
		t.Errorf("expected runtime 120, got %d", result.Runtime)
	}
	if result.Director != "Director X" {
		t.Errorf("expected Director X, got %s", result.Director)
	}
	if result.Maker != "Maker X" {
		t.Errorf("expected Maker X, got %s", result.Maker)
	}
	if result.Label != "Label X" {
		t.Errorf("expected Label X, got %s", result.Label)
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

func TestExtractInfoValueFinal(t *testing.T) {
	htmlStr := `<html><body>
<div id="info">
<p><span class="header">品番:</span> ABC-123</p>
<p><span class="header">発売日:</span> 2024-01-15</p>
</div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	if v := extractInfoValue(doc, []string{"品番"}); v != "ABC-123" {
		t.Errorf("expected ABC-123, got %q", v)
	}
	if v := extractInfoValue(doc, []string{"発売日"}); v != "2024-01-15" {
		t.Errorf("expected 2024-01-15, got %q", v)
	}
}

func TestExtractInfoLinkValueFinal(t *testing.T) {
	htmlStr := `<html><body>
<div id="info">
<p><span class="header">監督:</span> <a>Director Name</a></p>
</div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	if v := extractInfoLinkValue(doc, []string{"監督"}); v != "Director Name" {
		t.Errorf("expected Director Name, got %q", v)
	}
}

func TestExtractActressesFinal_JapaneseName(t *testing.T) {
	htmlStr := `<html><body>
<div id="star-div"><a href="/star/abc"><img title="波多野結衣" src="thumb1.jpg"></a></div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	actresses := extractActresses(doc)
	if len(actresses) != 1 {
		t.Fatalf("expected 1 actress, got %d", len(actresses))
	}
	if actresses[0].JapaneseName != "波多野結衣" {
		t.Errorf("expected Japanese name, got %s", actresses[0].JapaneseName)
	}
}

func TestExtractActressesFinal_WesternName(t *testing.T) {
	htmlStr := `<html><body>
<div id="star-div"><a href="/star/abc"><img title="Jane Doe" src="thumb1.jpg"></a></div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	actresses := extractActresses(doc)
	if len(actresses) != 1 {
		t.Fatalf("expected 1 actress, got %d", len(actresses))
	}
	if actresses[0].FirstName != "Jane" || actresses[0].LastName != "Doe" {
		t.Errorf("expected Jane Doe, got FirstName=%s LastName=%s", actresses[0].FirstName, actresses[0].LastName)
	}
}

func TestExtractActressesFinal_InvalidNames(t *testing.T) {
	htmlStr := `<html><body>
<div id="star-div"><a href="/star/abc"><img title="出演者" src="thumb1.jpg"></a></div>
<div id="star-div"><a href="/star/def"><img title="画像を拡大" src="thumb2.jpg"></a></div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	actresses := extractActresses(doc)
	if len(actresses) != 0 {
		t.Errorf("expected 0 actresses (invalid names), got %d", len(actresses))
	}
}

func TestExtractGenresFinal(t *testing.T) {
	htmlStr := `<html><body>
<div id="genre-toggle"><a>Genre1</a><a>Genre2</a></div>
<div id="info"><a href="/genre/abc">Genre3</a></div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	genres := extractGenres(doc)
	if len(genres) != 3 {
		t.Fatalf("expected 3 genres, got %d", len(genres))
	}
}

func TestExtractCoverURLFinal(t *testing.T) {
	htmlStr := `<html><body>
<a class="bigImage" href="https://pics.javbus.com/cover.jpg">Cover</a>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	url := extractCoverURL(doc, "https://www.javbus.com")
	if url == "" {
		t.Error("expected cover URL")
	}
}

func TestExtractScreenshotURLsFinal(t *testing.T) {
	htmlStr := `<html><body>
<a class="sample-box" href="https://pics.javbus.com/sample1.jpg">1</a>
<a class="sample-box" href="https://pics.javbus.com/sample2.jpg">2</a>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	urls := extractScreenshotURLs(doc, "https://www.javbus.com")
	if len(urls) != 2 {
		t.Fatalf("expected 2 screenshots, got %d", len(urls))
	}
}

func TestExtractTrailerURLFinal(t *testing.T) {
	htmlStr := `<html><body>
<video><source src="https://www.javbus.com/trailer.mp4"></video>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	url := extractTrailerURL(doc)
	if url != "https://www.javbus.com/trailer.mp4" {
		t.Errorf("expected trailer URL, got %s", url)
	}
}

func TestExtractDescriptionFinal(t *testing.T) {
	htmlStr := `<html><head>
<meta name="description" content="This is a movie description">
</head></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	desc := extractDescription(doc)
	if desc != "This is a movie description" {
		t.Errorf("expected description, got %q", desc)
	}
}

func TestIsLikelyImageURLFinal(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://example.com/image.jpg", true},
		{"https://example.com/image.png", true},
		{"https://example.com/image.webp", true},
		{"https://example.com/image.gif", true},
		{"https://example.com/image.bmp", true},
		{"https://example.com/image.avif", true},
		{"https://example.com/image.svg", false},
		{"https://example.com/page.html", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isLikelyImageURL(tt.url)
		if got != tt.want {
			t.Errorf("isLikelyImageURL(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestIsJavbusChallengePageFinal(t *testing.T) {
	tests := []struct {
		html string
		want bool
	}{
		{"<html>/doc/driver-verify</html>", true},
		{"<html>age verification javbus</html>", true},
		{"<html>driver-verify?referer=abc</html>", true},
		{"<html>normal page</html>", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isJavbusChallengePage(tt.html)
		if got != tt.want {
			t.Errorf("isJavbusChallengePage() = %v, want %v", got, tt.want)
		}
	}
}

func TestNormalizeLanguageFinal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"en", "en"},
		{"ja", "ja"},
		{"zh", "zh"},
		{"cn", "zh"},
		{"tw", "zh"},
		{"fr", "zh"},
		{"", "zh"},
	}
	for _, tt := range tests {
		got := normalizeLanguage(tt.input)
		if got != tt.want {
			t.Errorf("normalizeLanguage(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIdsMatchFinal(t *testing.T) {
	tests := []struct {
		candidate string
		target    string
		want      bool
	}{
		{"ABC-123", "abc123", true},
		{"abc123", "abc123", true},
		{"", "abc123", false},
		{"XYZ-999", "abc123", false},
	}
	for _, tt := range tests {
		got := idsMatch(tt.candidate, tt.target)
		if got != tt.want {
			t.Errorf("idsMatch(%q, %q) = %v, want %v", tt.candidate, tt.target, got, tt.want)
		}
	}
}

func TestExtractInfoValueFinal_ColonFormat(t *testing.T) {
	htmlStr := `<html><body>
<div id="info">
<p>品番: ABC-456</p>
</div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	v := extractInfoValue(doc, []string{"品番"})
	// The colon format may include the label prefix; check it contains the ID
	if !strings.Contains(v, "ABC-456") {
		t.Errorf("expected value to contain ABC-456, got %q", v)
	}
}

func TestApplyLanguageToURLFinal(t *testing.T) {
	s := &scraper{language: "en"}
	result := s.applyLanguageToURL("https://www.javbus.com/ABC-123")
	if !strings.Contains(result, "/en/") {
		t.Errorf("expected /en/ in URL, got %s", result)
	}
}
