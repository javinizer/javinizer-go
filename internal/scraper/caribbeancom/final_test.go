package caribbeancom

import (
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func TestParseDetailPageFinal_FullJapanesePage(t *testing.T) {
	htmlStr := `<html><body>
<script>var Movie = {"movie_id":"010122-001"};</script>
<h1 itemprop="name">テスト動画タイトル</h1>
<p itemprop="description">テスト説明文</p>
<li class="movie-spec"><span class="spec-title">配信日：</span><span class="spec-content">2021/01/01</span></li>
<li class="movie-spec"><span class="spec-title">再生時間：</span><span class="spec-content">PT1H30M</span></li>
<li class="movie-spec"><span class="spec-title">出演：</span><span class="spec-content"><a itemprop="actor"><span itemprop="name">テスト女優</span></a></span></li>
<li class="movie-spec"><span class="spec-title">タグ：</span><span class="spec-content"><a>タグ1</a><a>タグ2</a></span></li>
<meta property="og:image" content="https://www.caribbeancom.com/moviepages/010122-001/images/l_l.jpg">
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	result := parseDetailPage(doc, htmlStr, "https://www.caribbeancom.com/moviepages/010122-001/index.html", "010122-001", "ja")
	if result.ID != "010122-001" {
		t.Errorf("expected ID 010122-001, got %s", result.ID)
	}
	if result.Runtime != 90 {
		t.Errorf("expected runtime 90, got %d", result.Runtime)
	}
	if result.Title != "テスト動画タイトル" {
		t.Errorf("expected title, got %s", result.Title)
	}
	if len(result.Actresses) != 1 {
		t.Errorf("expected 1 actress, got %d", len(result.Actresses))
	}
	if len(result.Genres) != 2 {
		t.Errorf("expected 2 genres, got %d", len(result.Genres))
	}
}

func TestExtractCoverURLFinal_OGImage(t *testing.T) {
	htmlStr := `<html><body>
<meta property="og:image" content="https://www.caribbeancom.com/moviepages/010122-001/images/l_l.jpg">
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	url := extractCoverURL(doc, htmlStr, "https://www.caribbeancom.com/moviepages/010122-001/index.html", "010122-001")
	if url == "" {
		t.Error("expected cover URL from og:image")
	}
}

func TestExtractScreenshotsFinal_WithGallery(t *testing.T) {
	htmlStr := `<html><body>
<a class="fancy-gallery" href="/moviepages/010122-001/images/01.jpg" data-is_sample="1">img1</a>
<a class="fancy-gallery" href="/moviepages/010122-001/images/02.jpg" data-is_sample="1">img2</a>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	urls := extractScreenshots(doc, "https://www.caribbeancom.com")
	if len(urls) != 2 {
		t.Fatalf("expected 2 screenshots, got %d", len(urls))
	}
}

func TestExtractScreenshotsFinal_SkipsNonSample(t *testing.T) {
	htmlStr := `<html><body>
<a class="fancy-gallery" href="/moviepages/010122-001/images/01.jpg" data-is_sample="0">img1</a>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	urls := extractScreenshots(doc, "https://www.caribbeancom.com")
	if len(urls) != 0 {
		t.Fatalf("expected 0 screenshots (non-sample), got %d", len(urls))
	}
}

func TestExtractTrailerURLFinal_FromJSON(t *testing.T) {
	html := `<script>var sample_flash_url = "https://www.caribbeancom.com/moviepages/010122-001/sample.mp4";</script>`
	url := extractTrailerURL(html, "https://www.caribbeancom.com")
	if url == "" {
		t.Error("expected trailer URL from JSON")
	}
}

func TestExtractTrailerURLFinal_FromAssign(t *testing.T) {
	html := `<script>sample_flash_url = 'https://www.caribbeancom.com/sample2.mp4';</script>`
	url := extractTrailerURL(html, "https://www.caribbeancom.com")
	if url == "" {
		t.Error("expected trailer URL from assignment")
	}
}

func TestExtractTrailerURLFinal_EscapedChars(t *testing.T) {
	html := `<script>var sample_flash_url = "https:\/\/www.caribbeancom.com\/sample.mp4";</script>`
	url := extractTrailerURL(html, "https://www.caribbeancom.com")
	if url == "" {
		t.Error("expected trailer URL with escaped chars")
	}
	if strings.Contains(url, `\/`) {
		t.Errorf("expected unescaped URL, got %s", url)
	}
}

func TestParseRuntimeFinal_ISOFormat(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"PT1H30M", 90},
		{"PT2H", 120},
		{"PT45M", 45},
		{"PT1H30M45S", 91},
		{"PT30S", 1},
	}
	for _, tt := range tests {
		got := parseRuntime(tt.input)
		if got != tt.want {
			t.Errorf("parseRuntime(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseRuntimeFinal_ClockFormat(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"1:30:00", 90},
		{"0:45:00", 45},
		{"2:00:00", 120},
		{"1:30:45", 91},
	}
	for _, tt := range tests {
		got := parseRuntime(tt.input)
		if got != tt.want {
			t.Errorf("parseRuntime(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseRuntimeFinal_MinuteFormat(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"90 min", 90},
		{"45分", 45},
		{"120 minutes", 120},
	}
	for _, tt := range tests {
		got := parseRuntime(tt.input)
		if got != tt.want {
			t.Errorf("parseRuntime(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseReleaseDateFinal(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"2021-01-15", true},
		{"2021/01/15", true},
		{"", false},
		{"invalid", false},
	}
	for _, tt := range tests {
		result := parseReleaseDate(tt.input)
		if (result != nil) != tt.want {
			t.Errorf("parseReleaseDate(%q) = %v, want non-nil=%v", tt.input, result, tt.want)
		}
	}
}

func TestParseReleaseDateFromIDFinal(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"010122-001", true},
		{"invalid", false},
		{"991301-001", false}, // invalid month 99
	}
	for _, tt := range tests {
		result := parseReleaseDateFromID(tt.id)
		if (result != nil) != tt.want {
			t.Errorf("parseReleaseDateFromID(%q) non-nil=%v, want %v", tt.id, result != nil, tt.want)
		}
	}
}

func TestStripSiteSuffixFinal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Test Title | 無修正アダルト動画 カリビアンコム", "Test Title"},
		{"Test Title | Caribbeancom", "Test Title"},
		{"No Suffix", "No Suffix"},
	}
	for _, tt := range tests {
		got := stripSiteSuffix(tt.input)
		if got != tt.want {
			t.Errorf("stripSiteSuffix(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsMovieDetailPageFinal_404Class(t *testing.T) {
	htmlStr := `<html><body><div class="error404-wrap">Not Found</div></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	if isMovieDetailPage(doc, htmlStr) {
		t.Error("expected page to be detected as 404")
	}
}

func TestIsMovieDetailPageFinal_NullMovie(t *testing.T) {
	htmlStr := `<html><body><script>var Movie = null;</script></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	if isMovieDetailPage(doc, htmlStr) {
		t.Error("expected page with null Movie to not be a detail page")
	}
}

func TestIsMovieDetailPageFinal_ValidMovie(t *testing.T) {
	htmlStr := `<html><body><script>var Movie = {"movie_id":"010122-001"};</script></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	if !isMovieDetailPage(doc, htmlStr) {
		t.Error("expected page with valid Movie to be a detail page")
	}
}

func TestNormalizeMovieIDFinal(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"010122_001", "010122-001"},
		{"010122-1", "010122-1"}, // 2-digit suffix not zero-padded by normalizer
		{"010122-01", "010122-001"},
		{"CARIB-010122_001", "010122-001"},
	}
	for _, tt := range tests {
		got := normalizeMovieID(tt.input)
		if got != tt.want {
			t.Errorf("normalizeMovieID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExtractSpecValueFinal(t *testing.T) {
	htmlStr := `<html><body>
<li class="movie-spec"><span class="spec-title">配信日：</span><span class="spec-content">2021/01/01</span></li>
<li class="movie-spec"><span class="spec-title">再生時間：</span><span class="spec-content">90 min</span></li>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	if v := extractSpecValue(doc, []string{"配信日"}); v != "2021/01/01" {
		t.Errorf("expected date value, got %q", v)
	}
	if v := extractSpecValue(doc, []string{"再生時間"}); v != "90 min" {
		t.Errorf("expected runtime value, got %q", v)
	}
	if v := extractSpecValue(doc, []string{"存在しない"}); v != "" {
		t.Errorf("expected empty for missing label, got %q", v)
	}
}

func TestExtractActressesFinal_IgnoresRelated(t *testing.T) {
	htmlStr := `<html><body>
<div class="movie-info">
<li class="movie-spec"><span class="spec-title">出演：</span><span class="spec-content">
<a itemprop="actor"><span itemprop="name">女優A</span></a>
<a itemprop="actor"><span itemprop="name">女優B</span></a>
</span></li>
</div>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	actresses := extractActresses(doc)
	if len(actresses) != 2 {
		t.Fatalf("expected 2 actresses, got %d", len(actresses))
	}
}

func TestExtractGenresFinal(t *testing.T) {
	htmlStr := `<html><body>
<li class="movie-spec"><span class="spec-title">タグ：</span><span class="spec-content">
<a>Tag1</a><a>Tag2</a><a>Tag1</a>
</span></li>
</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}
	genres := extractGenres(doc)
	if len(genres) != 2 {
		t.Fatalf("expected 2 unique genres, got %d: %v", len(genres), genres)
	}
}

func TestApplyLanguageFinal_EnglishURL(t *testing.T) {
	s := &scraper{baseURL: "https://www.caribbeancom.com", language: "en"}
	result := s.applyLanguage("https://www.caribbeancom.com/moviepages/010122-001/index.html")
	if !strings.Contains(result, "en.caribbeancom.com") {
		t.Errorf("expected English host in URL, got %s", result)
	}
	if !strings.Contains(result, "/eng/") {
		t.Errorf("expected /eng/ path in URL, got %s", result)
	}
}

func TestApplyLanguageFinal_JapaneseURL(t *testing.T) {
	s := &scraper{baseURL: "https://www.caribbeancom.com", language: "ja"}
	result := s.applyLanguage("https://en.caribbeancom.com/eng/moviepages/010122-001/index.html")
	if strings.Contains(result, "en.caribbeancom.com") {
		t.Errorf("expected Japanese host in URL, got %s", result)
	}
	if strings.Contains(result, "/eng/") {
		t.Errorf("expected no /eng/ in URL, got %s", result)
	}
}

func TestAtoiSafeFinal(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"42", 42},
		{"", 0},
		{"abc", 0},
		{"  7  ", 7},
	}
	for _, tt := range tests {
		got := atoiSafe(tt.input)
		if got != tt.want {
			t.Errorf("atoiSafe(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeLanguageFinal(t *testing.T) {
	if normalizeLanguage("en") != "en" {
		t.Error("expected 'en'")
	}
	if normalizeLanguage("ja") != "ja" {
		t.Error("expected 'ja' for ja input")
	}
	if normalizeLanguage("") != "ja" {
		t.Error("expected 'ja' for empty input")
	}
	if normalizeLanguage("fr") != "ja" {
		t.Error("expected 'ja' for unsupported language")
	}
}

func TestParseReleaseDateFromIDFinal_Valid(t *testing.T) {
	result := parseReleaseDateFromID("010122-001")
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// 01=month, 01=day, 22=year (2022)
	if result.Year() != 2022 {
		t.Errorf("expected year 2022, got %d", result.Year())
	}
	if result.Month() != time.January {
		t.Errorf("expected January, got %v", result.Month())
	}
}
