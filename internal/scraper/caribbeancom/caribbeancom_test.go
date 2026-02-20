package caribbeancom

import (
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/config"
)

func TestResolveSearchQuery(t *testing.T) {
	s := New(config.DefaultConfig())

	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{name: "direct id", input: "120614-753", want: "120614-753", ok: true},
		{name: "underscore id", input: "120614_753", want: "120614-753", ok: true},
		{name: "movie page url", input: "https://www.caribbeancom.com/moviepages/120614-753/index.html", want: "120614-753", ok: true},
		{name: "embedded token", input: "120614_753-carib-1080p", want: "120614-753", ok: true},
		{name: "invalid", input: "abc-123", want: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := s.ResolveSearchQuery(tt.input)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("ResolveSearchQuery(%q) = (%q, %v), want (%q, %v)", tt.input, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestParseDetailPageJapanese(t *testing.T) {
	html := `
<!doctype html>
<html>
<head>
<title>慟哭の女教師 前編 〜だらしなく砕け散るプライド〜 | 無修正アダルト動画 カリビアンコム</title>
<meta name="description" content="説明文です">
</head>
<body>
<h1 itemprop="name">慟哭の女教師 前編 〜だらしなく砕け散るプライド〜</h1>
<p itemprop="description">事件のあった補習授業から数週間。</p>
<ul>
  <li class="movie-spec">
    <span class="spec-title">出演</span>
    <span class="spec-content"><a itemprop="actor"><span itemprop="name">大橋未久</span></a></span>
  </li>
  <li class="movie-spec">
    <span class="spec-title">再生時間</span>
    <span class="spec-content"><span itemprop="duration" content="T00H55M51S">00:55:51</span></span>
  </li>
  <li class="movie-spec">
    <span class="spec-title">タグ</span>
    <span class="spec-content"><a class="spec-item">中出し</a><a class="spec-item">女教師</a></span>
  </li>
</ul>
<script>
var Movie = {"movie_id":"120614-753","sample_flash_url":"https:\/\/smovie.caribbeancom.com\/sample\/movies\/120614-753\/480p.mp4"};
</script>
<div class="movie-gallery">
  <a class="gallery-item fancy-gallery" href="/moviepages/120614-753/images/l/001.jpg" data-is_sample="1"></a>
  <a class="gallery-item fancy-gallery" href="/member/moviepages/120614-753/images/l/006.jpg" data-is_sample="0"></a>
</div>
</body>
</html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("failed to parse html: %v", err)
	}

	result := parseDetailPage(doc, html, "https://www.caribbeancom.com/moviepages/120614-753/index.html", "120614-753", "ja")

	if result.Source != "caribbeancom" {
		t.Fatalf("Source = %q, want caribbeancom", result.Source)
	}
	if result.ID != "120614-753" {
		t.Fatalf("ID = %q, want 120614-753", result.ID)
	}
	if result.Title == "" || !strings.Contains(result.Title, "慟哭の女教師") {
		t.Fatalf("unexpected Title: %q", result.Title)
	}
	if result.Runtime != 56 {
		t.Fatalf("Runtime = %d, want 56", result.Runtime)
	}
	if len(result.Actresses) != 1 || result.Actresses[0].JapaneseName != "大橋未久" {
		t.Fatalf("unexpected actresses: %+v", result.Actresses)
	}
	if len(result.Genres) != 2 {
		t.Fatalf("Genres length = %d, want 2", len(result.Genres))
	}
	if len(result.ScreenshotURL) != 1 {
		t.Fatalf("ScreenshotURL length = %d, want 1", len(result.ScreenshotURL))
	}
	if !strings.Contains(result.CoverURL, "/moviepages/120614-753/images/l_l.jpg") {
		t.Fatalf("unexpected CoverURL: %q", result.CoverURL)
	}
	if !strings.Contains(result.TrailerURL, "smovie.caribbeancom.com/sample/movies/120614-753/480p.mp4") {
		t.Fatalf("unexpected TrailerURL: %q", result.TrailerURL)
	}
	if result.ReleaseDate == nil {
		t.Fatalf("ReleaseDate should not be nil")
	}
	wantDate := time.Date(2014, 12, 6, 0, 0, 0, 0, time.UTC)
	if !result.ReleaseDate.Equal(wantDate) {
		t.Fatalf("ReleaseDate = %v, want %v", result.ReleaseDate, wantDate)
	}
}

func TestParseDetailPageEnglish(t *testing.T) {
	html := `
<!doctype html>
<html>
<head><title>Crying Teacher Part I: The Crumbled Pride | Caribbeancom</title></head>
<body>
<h1 itemprop="name">Crying Teacher Part I: The Crumbled Pride</h1>
<ul>
  <li class="movie-detail__spec">
    <span class="spec-title">Starring:</span>
    <span class="spec-content"><a itemprop="actor"><span itemprop="name">Miku Ohashi</span></a></span>
  </li>
  <li class="movie-detail__spec">
    <span class="spec-title">Tags:</span>
    <span class="spec-content"><a class="spec__tag">creampie</a></span>
  </li>
  <li class="movie-detail__spec">
    <span class="spec-title">Duration:</span>
    <span class="spec-content"><span itemprop="duration" content="T00H40M10S">00:40:10</span></span>
  </li>
</ul>
<script>var Movie = {"movie_id":"120614-753"};</script>
</body>
</html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("failed to parse html: %v", err)
	}

	result := parseDetailPage(doc, html, "https://en.caribbeancom.com/eng/moviepages/120614-753/index.html", "120614-753", "en")

	if result.Language != "en" {
		t.Fatalf("Language = %q, want en", result.Language)
	}
	if result.Runtime != 40 {
		t.Fatalf("Runtime = %d, want 40", result.Runtime)
	}
	if len(result.Actresses) != 1 || result.Actresses[0].JapaneseName != "Miku Ohashi" {
		t.Fatalf("unexpected actresses: %+v", result.Actresses)
	}
	if len(result.Genres) != 1 || result.Genres[0] != "creampie" {
		t.Fatalf("unexpected genres: %+v", result.Genres)
	}
}

func TestExtractActresses_IgnoresRelatedActors(t *testing.T) {
	html := `
<!doctype html>
<html>
<body>
<div id="moviepages">
  <div class="movie-info section">
    <ul>
      <li class="movie-spec">
        <span class="spec-title">出演</span>
        <span class="spec-content"><a itemprop="actor"><span itemprop="name">大橋未久</span></a></span>
      </li>
    </ul>
  </div>
  <div class="movie-related sidebar">
    <a itemprop="actor"><span itemprop="name">加藤えま</span></a>
    <a itemprop="actor"><span itemprop="name">木村つな</span></a>
  </div>
</div>
</body>
</html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("failed to parse html: %v", err)
	}

	actresses := extractActresses(doc)
	if len(actresses) != 1 {
		t.Fatalf("len(actresses) = %d, want 1; got %+v", len(actresses), actresses)
	}
	if actresses[0].JapaneseName != "大橋未久" {
		t.Fatalf("first actress = %q, want 大橋未久", actresses[0].JapaneseName)
	}
}

func TestApplyLanguage(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Scrapers.Caribbeancom.Language = "en"
	sEn := New(cfg)

	cfgJa := config.DefaultConfig()
	cfgJa.Scrapers.Caribbeancom.Language = "ja"
	sJa := New(cfgJa)

	enURL := sEn.applyLanguage("https://www.caribbeancom.com/moviepages/120614-753/index.html")
	if enURL != "https://en.caribbeancom.com/eng/moviepages/120614-753/index.html" {
		t.Fatalf("unexpected en URL: %s", enURL)
	}

	jaURL := sJa.applyLanguage("https://en.caribbeancom.com/eng/moviepages/120614-753/index.html")
	if jaURL != "https://www.caribbeancom.com/moviepages/120614-753/index.html" {
		t.Fatalf("unexpected ja URL: %s", jaURL)
	}
}
