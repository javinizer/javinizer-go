package tokyohot

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestParseDetailPageFinal_FullPage(t *testing.T) {
	htmlStr := `<html><head><title>Test Movie | Tokyo Hot</title></head><body>
<dl class="info">
<dt>Product ID</dt><dd>n0679</dd>
<dt>配信開始日</dt><dd>2024/01/15</dd>
<dt>収録時間</dt><dd>01:30:00</dd>
<dt>メーカー</dt><dd><a>Maker X</a></dd>
<dt>シリーズ</dt><dd><a>Series X</a></dd>
<dt>Model</dt><dd>Actress A, Actress B</dd>
<dt>Play</dt><dd>Genre1, Genre2</dd>
</dl>
<div class="sentence">Test description</div>
<img src="https://www.tokyo-hot.com/jacket.jpg">
<div class="scap"><a href="https://www.tokyo-hot.com/screenshot1.jpg">1</a></div>
<video><source src="https://www.tokyo-hot.com/trailer.mp4"></video>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	result := parseDetailPage(doc, "https://www.tokyo-hot.com/product/n0679/", "n0679", "ja")
	if result.ID != "N0679" {
		t.Errorf("expected ID N0679, got %s", result.ID)
	}
	if result.Title != "Test Movie" {
		t.Errorf("expected Test Movie, got %s", result.Title)
	}
	if result.Runtime != 90 {
		t.Errorf("expected runtime 90, got %d", result.Runtime)
	}
	if result.Maker != "Maker X" {
		t.Errorf("expected Maker X, got %s", result.Maker)
	}
	if result.Series != "Series X" {
		t.Errorf("expected Series X, got %s", result.Series)
	}
	if result.Description != "Test description" {
		t.Errorf("expected description, got %s", result.Description)
	}
	if len(result.Actresses) < 2 {
		t.Errorf("expected at least 2 actresses, got %d", len(result.Actresses))
	}
	if len(result.Genres) != 2 {
		t.Errorf("expected 2 genres, got %d", len(result.Genres))
	}
}

func TestExtractInfoDDFinal(t *testing.T) {
	htmlStr := `<html><body>
<dl class="info">
<dt>Product ID</dt><dd>TEST-001</dd>
<dt>配信開始日</dt><dd>2024/01/15</dd>
</dl>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	if v := extractInfoDD(doc, []string{"Product ID"}); v != "TEST-001" {
		t.Errorf("expected TEST-001, got %q", v)
	}
	if v := extractInfoDD(doc, []string{"配信開始日"}); v != "2024/01/15" {
		t.Errorf("expected date, got %q", v)
	}
}

func TestExtractInfoLinkValueFinal(t *testing.T) {
	htmlStr := `<html><body>
<dl class="info">
<dt>メーカー</dt><dd><a>Maker Name</a></dd>
</dl>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	if v := extractInfoLinkValue(doc, []string{"メーカー"}); v != "Maker Name" {
		t.Errorf("expected Maker Name, got %q", v)
	}
}

func TestExtractActressesFinal(t *testing.T) {
	htmlStr := `<html><body>
<dl class="info">
<dt>Model</dt><dd>波多野結衣, Jane Doe</dd>
</dl>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	actresses := extractActresses(doc)
	if len(actresses) < 2 {
		t.Fatalf("expected at least 2 actresses, got %d", len(actresses))
	}
	if actresses[0].JapaneseName != "波多野結衣" {
		t.Errorf("expected Japanese name, got %s", actresses[0].JapaneseName)
	}
}

func TestExtractGenresFinal(t *testing.T) {
	htmlStr := `<html><body>
<dl class="info">
<dt>Play</dt><dd>Genre1, Genre2</dd>
</dl>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	genres := extractGenres(doc)
	if len(genres) != 2 {
		t.Fatalf("expected 2 genres, got %d", len(genres))
	}
}

func TestExtractCoverURLFinal(t *testing.T) {
	htmlStr := `<html><body>
<img src="https://www.tokyo-hot.com/jacket/n0679.jpg">
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	url := extractCoverURL(doc, "https://www.tokyo-hot.com")
	if url == "" {
		t.Error("expected cover URL")
	}
}

func TestExtractCoverURLFinal_VideoPoster(t *testing.T) {
	htmlStr := `<html><body>
<video poster="https://www.tokyo-hot.com/poster.jpg"></video>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	url := extractCoverURL(doc, "https://www.tokyo-hot.com")
	if url == "" {
		t.Error("expected cover URL from video poster")
	}
}

func TestExtractScreenshotURLsFinal(t *testing.T) {
	htmlStr := `<html><body>
<div class="scap"><a href="https://www.tokyo-hot.com/cap1.jpg">1</a></div>
<div class="scap"><a href="https://www.tokyo-hot.com/cap2.jpg">2</a></div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	urls := extractScreenshotURLs(doc, "https://www.tokyo-hot.com")
	if len(urls) != 2 {
		t.Fatalf("expected 2 screenshots, got %d", len(urls))
	}
}

func TestExtractTrailerURLFinal(t *testing.T) {
	htmlStr := `<html><body>
<video><source src="https://www.tokyo-hot.com/trailer.mp4"></video>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	url := extractTrailerURL(doc, "https://www.tokyo-hot.com")
	if url == "" {
		t.Error("expected trailer URL")
	}
}

func TestSplitNamesFinal(t *testing.T) {
	result := splitNames("A, B、C/D")
	if len(result) != 4 {
		t.Fatalf("expected 4 names, got %d", len(result))
	}
	if result[0] != "A" {
		t.Errorf("expected A, got %s", result[0])
	}
}

func TestExtractIDFinal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"n0679", "N0679"},
		{"kb-1234", "KB-1234"},
		{"12345", ""}, // digits-only doesn't match [A-Za-z]+ pattern
		{"", ""},
	}
	for _, tt := range tests {
		got := extractID(tt.input)
		if got != tt.want {
			t.Errorf("extractID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseDetailPageFinal_RuntimeMinutes(t *testing.T) {
	htmlStr := `<html><head><title>Test</title></head><body>
<dl class="info">
<dt>収録時間</dt><dd>90</dd>
</dl>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	result := parseDetailPage(doc, "https://www.tokyo-hot.com/product/test/", "TEST-001", "ja")
	if result.Runtime != 90 {
		t.Errorf("expected runtime 90, got %d", result.Runtime)
	}
}

func TestParseDetailPageFinal_EmptyPage(t *testing.T) {
	htmlStr := `<html><body></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	result := parseDetailPage(doc, "https://www.tokyo-hot.com/product/test/", "TEST-001", "ja")
	if result.ID != "TEST-001" {
		t.Errorf("expected fallback ID, got %s", result.ID)
	}
	if result.Title != "TEST-001" {
		t.Errorf("expected fallback title, got %s", result.Title)
	}
}
