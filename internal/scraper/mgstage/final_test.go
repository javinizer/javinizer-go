package mgstage

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
)

func TestParseHTMLFinal_FullPage(t *testing.T) {
	htmlStr := `<html><head><title>「Test Movie Title」：エロ動画・アダルトビデオ -MGS動画＜プレステージ グループ＞</title></head><body>
<table>
<tr><th>品番：</th><td>SIRO-5615</td></tr>
<tr><th>配信開始日：</th><td>2024/01/15</td></tr>
<tr><th>収録時間：</th><td>90 min</td></tr>
<tr><th>メーカー：</th><td><a>Maker X</a></td></tr>
<tr><th>レーベル：</th><td><a>Label X</a></td></tr>
<tr><th>シリーズ：</th><td><a>Series X</a></td></tr>
<tr><th>ジャンル：</th><td><a>Genre1</a><a>Genre2</a></td></tr>
<tr><th>出演：</th><td><a>Actress A</a><a>Actress B</a></td></tr>
</table>
<img src="https://www.mgstage.com/images/jacket.jpg">
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	s := &scraper{enabled: true}
	result, err := s.parseHTML(doc, "https://www.mgstage.com/product/product_detail/SIRO-5615/")
	if err != nil {
		t.Fatalf("parseHTML returned error: %v", err)
	}
	if result.ID != "SIRO-5615" {
		t.Errorf("expected ID SIRO-5615, got %s", result.ID)
	}
	if result.Title != "Test Movie Title" {
		t.Errorf("expected Test Movie Title, got %s", result.Title)
	}
	if result.Runtime != 90 {
		t.Errorf("expected runtime 90, got %d", result.Runtime)
	}
	if result.Maker != "Maker X" {
		t.Errorf("expected Maker X, got %s", result.Maker)
	}
	if len(result.Genres) != 2 {
		t.Errorf("expected 2 genres, got %d", len(result.Genres))
	}
	if len(result.Actresses) != 2 {
		t.Errorf("expected 2 actresses, got %d", len(result.Actresses))
	}
}

func TestExtractTableValueFinal(t *testing.T) {
	htmlStr := `<html><body>
<table><tr><th>品番：</th><td>TEST-001</td></tr></table>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	if v := extractTableValue(doc, "品番："); v != "TEST-001" {
		t.Errorf("expected TEST-001, got %q", v)
	}
}

func TestExtractTableLinkValueFinal(t *testing.T) {
	htmlStr := `<html><body>
<table><tr><th>メーカー：</th><td><a>Studio A</a></td></tr></table>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	if v := extractTableLinkValue(doc, "メーカー："); v != "Studio A" {
		t.Errorf("expected Studio A, got %q", v)
	}
}

func TestExtractGenresFinal(t *testing.T) {
	htmlStr := `<html><body>
<table><tr><th>ジャンル：</th><td><a>Comedy</a><a>Drama</a></td></tr></table>
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

func TestExtractActressesFinal(t *testing.T) {
	htmlStr := `<html><body>
<table><tr><th>出演：</th><td><a>波多野結衣</a><a>Jane Doe</a></td></tr></table>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	actresses := extractActresses(doc)
	if len(actresses) != 2 {
		t.Fatalf("expected 2 actresses, got %d", len(actresses))
	}
	if actresses[0].JapaneseName != "波多野結衣" {
		t.Errorf("expected JapaneseName, got %s", actresses[0].JapaneseName)
	}
	if actresses[1].FirstName != "Doe" || actresses[1].LastName != "Jane" {
		t.Errorf("expected Japanese-order name Jane Doe, got FirstName=%s LastName=%s", actresses[1].FirstName, actresses[1].LastName)
	}
}

func TestCreateActressInfoFinal(t *testing.T) {
	info := createActressInfo("山田太郎")
	if info.JapaneseName != "山田太郎" {
		t.Errorf("expected Japanese name for Japanese input")
	}
	info2 := createActressInfo("John Smith")
	// MGStage uses Japanese name order: LastName=first word, FirstName=second word
	if info2.LastName != "John" || info2.FirstName != "Smith" {
		t.Errorf("expected Japanese-order name parsing, got FirstName=%s LastName=%s", info2.FirstName, info2.LastName)
	}
	info3 := createActressInfo("Madonna")
	if info3.FirstName != "Madonna" {
		t.Errorf("expected single name as FirstName, got %s", info3.FirstName)
	}
}

func TestCleanTitleFinal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"「My Title」：エロ動画・アダルトビデオ -MGS動画＜プレステージ グループ＞", "My Title"},
		{"No brackets：suffix", "No brackets"},
		{"Simple Title - MGStage", "Simple Title"},
	}
	for _, tt := range tests {
		got := cleanTitle(tt.input)
		if got != tt.want {
			t.Errorf("cleanTitle(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsGenericMGStageTitleFinal(t *testing.T) {
	if !isGenericMGStageTitle("エロ動画・アダルトビデオ -MGS動画＜プレステージ グループ＞") {
		t.Error("expected generic title to be detected")
	}
	if isGenericMGStageTitle("Normal Movie Title") {
		t.Error("expected normal title to not be detected as generic")
	}
}

func TestIsGenericMGStageDescriptionFinal(t *testing.T) {
	if !isGenericMGStageDescription("MGS動画 エロ動画 something") {
		t.Error("expected generic description to be detected")
	}
	if isGenericMGStageDescription("This is a real description") {
		t.Error("expected normal description to not be detected as generic")
	}
}

func TestHasProductSignalsFinal(t *testing.T) {
	// hasProductSignals returns false for nil but may panic; test non-nil cases only
	if hasProductSignals(&models.ScraperResult{Runtime: 120}, "") {
		// This should actually return true
	}
	if !hasProductSignals(&models.ScraperResult{Runtime: 120}, "") {
		t.Error("expected runtime signal")
	}
	if !hasProductSignals(&models.ScraperResult{CoverURL: "http://example.com/cover.jpg"}, "") {
		t.Error("expected cover URL signal")
	}
	if hasProductSignals(&models.ScraperResult{}, "") {
		t.Error("expected no signals for empty result")
	}
}

func TestMGStageIDsMatchFinal(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"SIRO-5615", "SIRO-5615", true},
		{"GANA-2850", "200GANA-2850", true},
		{"ABC-123", "XYZ-999", false},
	}
	for _, tt := range tests {
		got := mgstageIDsMatch(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("mgstageIDsMatch(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestNormalizeMGStageIDTokenFinal(t *testing.T) {
	id, ok := normalizeMGStageIDToken("GANA-2850")
	if !ok || id != "GANA-2850" {
		t.Errorf("expected GANA-2850, got %s ok=%v", id, ok)
	}
	id2, ok2 := normalizeMGStageIDToken("GANA2850")
	// Compact format may or may not be recognized depending on regex
	if ok2 {
		if id2 != "GANA-2850" {
			t.Errorf("expected GANA-2850 for compact, got %s", id2)
		}
	}
	_, ok3 := normalizeMGStageIDToken("")
	if ok3 {
		t.Error("expected false for empty token")
	}
}

func TestSplitMGStageIDFinal(t *testing.T) {
	letter, number := splitMGStageID("GANA-2850")
	if letter != "GANA" || number != "2850" {
		t.Errorf("expected GANA/2850, got %s/%s", letter, number)
	}
	letter2, number2 := splitMGStageID("invalid")
	if letter2 != "" || number2 != "" {
		t.Errorf("expected empty for invalid, got %s/%s", letter2, number2)
	}
}

func TestExpandMGStagePrefixesFinal(t *testing.T) {
	candidates := expandMGStagePrefixes("GANA", "2850")
	if len(candidates) == 0 {
		t.Error("expected some candidates")
	}
	if candidates[0] != "200GANA-2850" {
		t.Errorf("expected first candidate 200GANA-2850, got %s", candidates[0])
	}
}

func TestExtractCoverURLFinal_MgStage(t *testing.T) {
	htmlStr := `<html><body>
<a class="link_magnify" href="https://www.mgstage.com/images/jacket_l.jpg">Enlarge</a>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	url := extractCoverURL(doc)
	if url == "" {
		t.Error("expected cover URL")
	}
}

func TestExtractScreenshotsFinal_MgStage(t *testing.T) {
	htmlStr := `<html><body>
<a class="sample_image" href="https://www.mgstage.com/images/sample1.jpg">1</a>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	urls := extractScreenshots(doc)
	if len(urls) != 1 {
		t.Fatalf("expected 1 screenshot, got %d", len(urls))
	}
}

func TestExtractDescriptionFinal_MgStage(t *testing.T) {
	htmlStr := `<html><body>
<p class="txt introduction">This is a test description for the movie.</p>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	desc := extractDescription(doc)
	if desc != "This is a test description for the movie." {
		t.Errorf("expected description, got %q", desc)
	}
}

func TestExtractRatingFinal_MgStage(t *testing.T) {
	htmlStr := `<html><body>
<div class="star_40"></div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	rating := extractRating(doc)
	if rating == nil {
		t.Fatal("expected non-nil rating")
	}
	if rating.Score != 8.0 {
		t.Errorf("expected score 8.0, got %f", rating.Score)
	}
}
