package dlgetchu

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
	cfg.Scrapers.DLGetchu.Enabled = true
	cfg.Scrapers.DLGetchu.BaseURL = baseURL
	cfg.Scrapers.DLGetchu.RequestDelay = 0
	cfg.Scrapers.Proxy.Enabled = false
	return cfg
}

func TestSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/") && r.URL.RawQuery == "search_keyword=ABC-123":
			fmt.Fprint(w, `<html><body><a href="/i/item12345">Result</a></body></html>`)
		case r.URL.Path == "/i/item12345":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<html><head>
<meta property="og:title" content="DLgetchu Sample Title">
<meta name="description" content="Fallback description">
</head><body>
<table>
<tr><td>作品内容</td><td>Long <b>description</b> for the DLgetchu parser.</td></tr>
</table>
<div>作品ID: 12345</div>
<div>発売日 2026/02/13</div>
<div>収録時間 ９０分</div>
<a href="dojin_circle_detail.php?id=44">Test Circle</a>
<a href="genre_id=1">Drama</a>
<a href="genre_id=2">Romance</a>
<img src="/data/item_img/demo/12345top.jpg">
"/data/item_img/demo/shot1.jpg" class="highslide"
"/data/item_img/demo/shot2.webp" class="highslide"
</body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := New(testConfig(server.URL))
	result, err := s.Search("ABC-123")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if result.Source != "dlgetchu" {
		t.Fatalf("Source = %q, want dlgetchu", result.Source)
	}
	if result.SourceURL != server.URL+"/i/item12345" {
		t.Fatalf("SourceURL = %q", result.SourceURL)
	}
	if result.ID != "12345" || result.ContentID != "12345" {
		t.Fatalf("unexpected IDs: %q %q", result.ID, result.ContentID)
	}
	if result.Title != "DLgetchu Sample Title" {
		t.Fatalf("Title = %q", result.Title)
	}
	if result.Description != "Long description for the DLgetchu parser." {
		t.Fatalf("Description = %q", result.Description)
	}
	if result.Maker != "Test Circle" {
		t.Fatalf("Maker = %q", result.Maker)
	}
	if result.Runtime != 90 {
		t.Fatalf("Runtime = %d, want 90", result.Runtime)
	}
	if result.ReleaseDate == nil {
		t.Fatal("ReleaseDate is nil")
	}
	wantDate := time.Date(2026, 2, 13, 0, 0, 0, 0, time.UTC)
	if !result.ReleaseDate.Equal(wantDate) {
		t.Fatalf("ReleaseDate = %v, want %v", result.ReleaseDate, wantDate)
	}
	if len(result.Genres) != 2 || result.Genres[0] != "Drama" || result.Genres[1] != "Romance" {
		t.Fatalf("Genres = %#v", result.Genres)
	}
	if len(result.ScreenshotURL) != 2 {
		t.Fatalf("ScreenshotURL len = %d, want 2", len(result.ScreenshotURL))
	}
	if result.CoverURL != server.URL+"/data/item_img/demo/12345top.jpg" || result.PosterURL != result.CoverURL {
		t.Fatalf("unexpected cover URLs: %q %q", result.CoverURL, result.PosterURL)
	}
}

func TestParseDetailPage_Fallbacks(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><head>
<title>Fallback Title</title>
<meta name="description" content="Meta fallback description">
</head><body></body></html>`))
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}

	result := parseDetailPage(doc, `<html><body><div>id=98765</div></body></html>`, "https://dl.getchu.com/i/item98765", "RJ-1")
	if result.ID != "98765" {
		t.Fatalf("ID = %q, want 98765", result.ID)
	}
	if result.Title != "Fallback Title" {
		t.Fatalf("Title = %q", result.Title)
	}
	if result.Description != "Meta fallback description" {
		t.Fatalf("Description = %q", result.Description)
	}
}

func TestHelpers(t *testing.T) {
	if got := findFirstDetailLink(`<a href="/i/item12345">x</a>`, "https://dl.getchu.com"); got != "https://dl.getchu.com/i/item12345" {
		t.Fatalf("findFirstDetailLink = %q", got)
	}
	if got := normalizeFullWidthDigits("１２３ ４５"); got != "123 45" {
		t.Fatalf("normalizeFullWidthDigits = %q", got)
	}
	if got := extractNumericID("作品ID: 54321"); got != "54321" {
		t.Fatalf("extractNumericID = %q", got)
	}
	if got := resolveURL("https://dl.getchu.com/i/item12345", "/x/y.jpg"); got != "https://dl.getchu.com/x/y.jpg" {
		t.Fatalf("resolveURL = %q", got)
	}
	if !isHTTPURL("https://dl.getchu.com/i/item12345") {
		t.Fatal("expected HTTP URL")
	}
}
