package aventertainment

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestParseDetailPageFinal_FullPage(t *testing.T) {
	htmlStr := `<html><head><title>Test Movie Title | AV Entertainment</title></head><body>
<div class="product-info-block-rev">
<div class="single-info">
<div class="title">商品番号</div>
<div class="value"><span class="tag-title">1PON-020326-001</span></div>
</div>
<div class="single-info">
<div class="title">主演女優</div>
<div class="value"><a href="/ppv_actressdetail?id=1">Actress A</a></div>
</div>
<div class="single-info">
<div class="title">スタジオ</div>
<div class="value"><a>Studio X</a></div>
</div>
<div class="single-info">
<div class="title">カテゴリ</div>
<div class="value"><a>Category1</a><a>Category2</a></div>
</div>
<div class="single-info">
<div class="title">発売日</div>
<div class="value">2024/01/15</div>
</div>
<div class="single-info">
<div class="title">収録時間</div>
<div class="value">1:30:00</div>
</div>
</div>
<div class="product-description">This is a test description.</div>
<a class="lightbox" href="/vodimages/screenshot/large/1PON-020326-001/01.jpg">Screenshot</a>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	result := parseDetailPage(doc, htmlStr, "https://www.aventertainments.com/ppv/detail/12345/", "", "en", false)
	if result.ID != "1PON-020326-001" {
		t.Errorf("expected ID 1PON-020326-001, got %s", result.ID)
	}
	if result.Runtime != 90 {
		t.Errorf("expected runtime 90, got %d", result.Runtime)
	}
	if result.Maker != "Studio X" {
		t.Errorf("expected Studio X, got %s", result.Maker)
	}
}

func TestExtractIDFinal_VariousFormats(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"ABC-123", "ABC-123"},
		{"1pon_020326_001", "1PON-020326-001"},
		{"carib_020326_001", "CARIB-020326-001"},
		{"ABC123", "ABC123"},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractID(tt.input)
		if got != tt.want {
			t.Errorf("extractID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeResolverInputFinal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"1PON_020326_001.mp4", "1pon_020326_001"},
		{"/path/to/CARIB-010122-001.avi", "carib-010122-001"},
		{"", ""},
	}
	for _, tt := range tests {
		got := normalizeResolverInput(tt.input)
		if got != tt.want {
			t.Errorf("normalizeResolverInput(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseDateFinal(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"2024-01-15", true},
		{"01/15/2024", true},
		{"2024/01/15", true},
		{"invalid", false},
	}
	for _, tt := range tests {
		result := parseDate(tt.input)
		if (result != nil) != tt.want {
			t.Errorf("parseDate(%q) non-nil=%v, want %v", tt.input, result != nil, tt.want)
		}
	}
}

func TestParseRuntimeFinal(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"1:30:00", 90},
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

func TestFindDateFinal(t *testing.T) {
	tests := []struct {
		html string
		want bool
	}{
		{`<span class="title">発売日</span><span class="value">2024/01/15</span>`, true},
		{`<span class="value">01/15/2024</span>`, true},
		{"no date info", false},
	}
	for _, tt := range tests {
		result := findDate(tt.html)
		if (result != "") != tt.want {
			t.Errorf("findDate() non-empty=%v, want %v", result != "", tt.want)
		}
	}
}

func TestFindRuntimeFinal(t *testing.T) {
	result := findRuntime(`<span class="title">収録時間</span><span class="value">1:30:00</span>`)
	if result == "" {
		t.Error("expected runtime to be found")
	}
}

func TestFindMakerFinal(t *testing.T) {
	html := `<span class="title">Studio</span><span class="value"><a>Studio Name</a></span>`
	result := findMaker(html)
	if result != "Studio Name" {
		t.Errorf("expected Studio Name, got %q", result)
	}
}

func TestNormalizeInfoLabelFinal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"商品番号：", "商品番号"},
		{"Studio", "studio"},
		{"Release Date", "releasedate"},
	}
	for _, tt := range tests {
		got := normalizeInfoLabel(tt.input)
		if got != tt.want {
			t.Errorf("normalizeInfoLabel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsProductIDLabelFinal(t *testing.T) {
	if !isProductIDLabel("商品番号") {
		t.Error("expected 商品番号 to match")
	}
	if !isProductIDLabel("productid") {
		t.Error("expected productid to match")
	}
	if isProductIDLabel("studio") {
		t.Error("expected studio to not match")
	}
}

func TestStripSiteSuffixFinal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Title - AV Entertainment", "Title"},
		{"Title | AV Entertainment", "Title"},
		{"No Suffix", "No Suffix"},
	}
	for _, tt := range tests {
		got := stripSiteSuffix(tt.input)
		if got != tt.want {
			t.Errorf("stripSiteSuffix(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExtractDetailLinksFinal(t *testing.T) {
	html := `<html><body>
<a href="/ppv/detail/12345">Movie 1</a>
<a href="/ppv/detail/67890">Movie 2</a>
<a href="/other/page">Other</a>
</body></html>`
	links := extractDetailLinks(html, "https://www.aventertainments.com")
	if len(links) != 2 {
		t.Fatalf("expected 2 detail links, got %d", len(links))
	}
}

func TestExtractCandidateIDFinal(t *testing.T) {
	tests := []struct {
		html string
		want string
	}{
		{`<span class="tag-title">ABC-123</span>`, "ABC-123"},
		{`item_no=XYZ-456`, "XYZ-456"},
		{"no id info", ""},
	}
	for _, tt := range tests {
		got := extractCandidateID(tt.html)
		if got != tt.want {
			t.Errorf("extractCandidateID() = %q, want %q", got, tt.want)
		}
	}
}

func TestIsAVEBonusScreenshotURLFinal(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"/vodimages/gallery/large/abc123/01.jpg", true},
		{"/vodimages/gallery/large/abc123/001.webp", true},
		{"/vodimages/screenshot/large/abc123/01.jpg", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isAVEBonusScreenshotURL(tt.url)
		if got != tt.want {
			t.Errorf("isAVEBonusScreenshotURL(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestNormalizeComparableIDFinal(t *testing.T) {
	if normalizeComparableID("dlABC-123") != "abc123" {
		t.Errorf("expected dl prefix stripped")
	}
	if normalizeComparableID("stXYZ-456") != "xyz456" {
		t.Errorf("expected st prefix stripped")
	}
}

func TestExtractDescriptionFinal(t *testing.T) {
	htmlStr := `<html><body>
<div class="product-description">This is a test description of the movie content.</div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	desc := extractDescription(doc)
	if desc != "This is a test description of the movie content." {
		t.Errorf("expected description, got %q", desc)
	}
}

func TestExtractGenresFromSelectionFinal(t *testing.T) {
	htmlStr := `<html><body>
<a href="?cat_id=1">Genre1</a>
<a href="?dept=2">Genre2</a>
<a href="/other">Not a genre</a>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	genres := extractGenres(doc.Selection)
	if len(genres) != 2 {
		t.Fatalf("expected 2 genres, got %d", len(genres))
	}
}

func TestExtractActressesFromSelectionFinal(t *testing.T) {
	htmlStr := `<html><body>
<a href="/ppv_actressdetail?id=1">波多野結衣</a>
<a href="/ppv_idoldetail?id=2">Jane Doe</a>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	actresses := extractActresses(doc.Selection)
	if len(actresses) < 1 {
		t.Fatalf("expected at least 1 actress, got %d", len(actresses))
	}
	if actresses[0].JapaneseName != "波多野結衣" {
		t.Errorf("expected Japanese name, got %s", actresses[0].JapaneseName)
	}
}
