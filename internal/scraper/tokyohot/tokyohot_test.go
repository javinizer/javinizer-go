package tokyohot

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/config"
)

func testConfig(baseURL string) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Scrapers.TokyoHot.Enabled = true
	cfg.Scrapers.TokyoHot.BaseURL = baseURL
	cfg.Scrapers.TokyoHot.Language = "en"
	cfg.Scrapers.TokyoHot.RequestDelay = 0
	cfg.Scrapers.Proxy.Enabled = false
	return cfg
}

func TestSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/product/":
			if got := r.URL.Query().Get("q"); got != "N1234" {
				t.Fatalf("query q = %q, want N1234", got)
			}
			fmt.Fprint(w, `<html><body><a href="/product/N1234/">N1234 Amazing Movie</a></body></html>`)
		case "/product/N1234/":
			fmt.Fprint(w, `<html><head><title>Amazing Movie | Tokyo-Hot</title></head><body>
<dl class="info">
  <dt>Product ID</dt><dd>N1234</dd>
  <dt>Release</dt><dd>2026/02/14</dd>
  <dt>Length</dt><dd>01:05:31</dd>
  <dt>Maker</dt><dd><a href="/maker/test">Tokyo Hot</a></dd>
  <dt>Series</dt><dd><a href="/series/test">Series X</a></dd>
  <dt>Model</dt><dd>Jane Doe / 花子</dd>
  <dt>Genre</dt><dd>Drama / Romance</dd>
</dl>
<div class="sentence">Story description for the TokyoHot parser test.</div>
<img src="/images/jacket.jpg">
<div class="scap"><a href="/gallery/1.jpg">one</a><a href="/gallery/2.jpg">two</a></div>
<video><source src="/trailers/n1234.mp4"></video>
</body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := New(testConfig(server.URL))
	result, err := s.Search("N1234")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if result.Source != "tokyohot" {
		t.Fatalf("Source = %q, want tokyohot", result.Source)
	}
	if result.SourceURL != server.URL+"/product/N1234/?lang=en" {
		t.Fatalf("SourceURL = %q", result.SourceURL)
	}
	if result.ID != "N1234" || result.ContentID != "N1234" {
		t.Fatalf("unexpected IDs: %q %q", result.ID, result.ContentID)
	}
	if result.Title != "Amazing Movie" {
		t.Fatalf("Title = %q", result.Title)
	}
	if result.Description != "Story description for the TokyoHot parser test." {
		t.Fatalf("Description = %q", result.Description)
	}
	if result.Maker != "Tokyo Hot" || result.Series != "Series X" {
		t.Fatalf("unexpected maker/series: %q %q", result.Maker, result.Series)
	}
	if result.Runtime != 66 {
		t.Fatalf("Runtime = %d, want 66", result.Runtime)
	}
	if result.ReleaseDate == nil {
		t.Fatal("ReleaseDate is nil")
	}
	wantDate := time.Date(2026, 2, 14, 0, 0, 0, 0, time.UTC)
	if !result.ReleaseDate.Equal(wantDate) {
		t.Fatalf("ReleaseDate = %v, want %v", result.ReleaseDate, wantDate)
	}
	if len(result.Genres) != 2 || result.Genres[0] != "Drama" || result.Genres[1] != "Romance" {
		t.Fatalf("Genres = %#v", result.Genres)
	}
	if len(result.Actresses) != 3 {
		t.Fatalf("Actresses len = %d, want 3", len(result.Actresses))
	}
	if result.Actresses[0].FirstName != "Jane" {
		t.Fatalf("unexpected first actress: %#v", result.Actresses[0])
	}
	if result.Actresses[1].FirstName != "Doe" {
		t.Fatalf("unexpected second actress: %#v", result.Actresses[1])
	}
	if result.Actresses[2].JapaneseName != "花子" {
		t.Fatalf("unexpected third actress: %#v", result.Actresses[2])
	}
	if result.CoverURL != server.URL+"/images/jacket.jpg" || result.PosterURL != result.CoverURL {
		t.Fatalf("unexpected cover URLs: %q %q", result.CoverURL, result.PosterURL)
	}
	if len(result.ScreenshotURL) != 2 {
		t.Fatalf("ScreenshotURL len = %d, want 2", len(result.ScreenshotURL))
	}
	if result.TrailerURL != server.URL+"/trailers/n1234.mp4" {
		t.Fatalf("TrailerURL = %q", result.TrailerURL)
	}
}

func TestParseDetailPage_Fallbacks(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><head><title>Fallback Title | Tokyo-Hot</title></head><body>
<dl class="info"><dt>Genre</dt><dd><a href="/genre/a">Action</a></dd></dl>
<img src="//cdn.example.com/jacket.jpg">
<img src="/thumb/vcap_1.jpg">
</body></html>`))
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}

	result := parseDetailPage(doc, "https://www.tokyo-hot.com/product/F9999/", "F9999", "zh")
	if result.Language != "zh" {
		t.Fatalf("Language = %q, want zh", result.Language)
	}
	if result.ID != "F9999" {
		t.Fatalf("ID = %q, want F9999", result.ID)
	}
	if result.CoverURL != "https://cdn.example.com/jacket.jpg" {
		t.Fatalf("CoverURL = %q", result.CoverURL)
	}
	if len(result.ScreenshotURL) != 1 || result.ScreenshotURL[0] != "https://www.tokyo-hot.com/thumb/vcap_1.jpg" {
		t.Fatalf("unexpected screenshots: %#v", result.ScreenshotURL)
	}
}

func TestHelpers(t *testing.T) {
	if got := normalizeLanguage("cn"); got != "zh" {
		t.Fatalf("normalizeLanguage = %q, want zh", got)
	}
	if got := extractID("TokyoHot N-1234 sample"); got != "N-1234" {
		t.Fatalf("extractID = %q, want N-1234", got)
	}
	if got := splitNames("Jane Doe / 花子"); len(got) != 3 {
		t.Fatalf("splitNames len = %d, want 3", len(got))
	}
	if got := resolveURL("https://www.tokyo-hot.com/product/N1234/", "trailer.mp4"); got != "https://www.tokyo-hot.com/product/N1234/trailer.mp4" {
		t.Fatalf("resolveURL = %q", got)
	}
	if !hasJapanese("花子") {
		t.Fatal("expected Japanese text detection")
	}
}
