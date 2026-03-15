package jav321

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
	cfg.Scrapers.Jav321.Enabled = true
	cfg.Scrapers.Jav321.BaseURL = baseURL
	cfg.Scrapers.Jav321.RequestDelay = 0
	cfg.Scrapers.Proxy.Enabled = false
	return cfg
}

func TestSearch(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/search":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := r.FormValue("sn"); got != "ABC-123" {
				t.Fatalf("form sn = %q, want ABC-123", got)
			}
			fmt.Fprintf(w, `<html><body><div>ABC-123 Great Movie <a href="%s/video/abc123">details</a></div></body></html>`, server.URL)
		case r.URL.Path == "/video/abc123":
			fmt.Fprint(w, `<html><head>
<meta property="og:title" content="Great Movie - JAV321">
<meta property="og:description" content="This is a sufficiently long description for the Jav321 parser test case.">
<meta property="og:image" content="/images/cover.jpg">
</head><body>
<div class="panel-heading"><h3>Great Movie ABC-123</h3></div>
<b>品番</b> : ABC-123<br>
<b>発売日</b> : 2026-02-03<br>
<b>収録時間</b> : 125 minutes<br>
<b>メーカー</b> : <a href="/maker/test">Test Studio</a><br>
<b>シリーズ</b> : <a href="/series/test">Test Series</a><br>
<b>出演者</b> : <a href="/actress/jane">Jane Doe</a> / <a href="/actress/hanako">花子</a><br>
<a href="/genre/drama">Drama</a>
<a href="/genre/romance">Romance</a>
<a href="/snapshot/1"><img src="/shots/1.jpg"></a>
<a href="/snapshot/2"><img src="/shots/2.jpg"></a>
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

	if result.Source != "jav321" {
		t.Fatalf("Source = %q, want jav321", result.Source)
	}
	if result.SourceURL != server.URL+"/video/abc123" {
		t.Fatalf("SourceURL = %q, want %q", result.SourceURL, server.URL+"/video/abc123")
	}
	if result.ID != "ABC-123" || result.ContentID != "ABC-123" {
		t.Fatalf("unexpected IDs: id=%q contentID=%q", result.ID, result.ContentID)
	}
	if result.Title != "Great Movie" {
		t.Fatalf("Title = %q, want Great Movie", result.Title)
	}
	if result.Maker != "Test Studio" {
		t.Fatalf("Maker = %q, want Test Studio", result.Maker)
	}
	if result.Series != "Test Series" {
		t.Fatalf("Series = %q, want Test Series", result.Series)
	}
	if result.Runtime != 125 {
		t.Fatalf("Runtime = %d, want 125", result.Runtime)
	}
	if result.ReleaseDate == nil {
		t.Fatal("ReleaseDate is nil")
	}
	wantDate := time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC)
	if !result.ReleaseDate.Equal(wantDate) {
		t.Fatalf("ReleaseDate = %v, want %v", result.ReleaseDate, wantDate)
	}
	if len(result.Genres) != 2 || result.Genres[0] != "Drama" || result.Genres[1] != "Romance" {
		t.Fatalf("Genres = %#v", result.Genres)
	}
	if len(result.ScreenshotURL) != 2 {
		t.Fatalf("ScreenshotURL len = %d, want 2", len(result.ScreenshotURL))
	}
	if result.CoverURL != server.URL+"/shots/1.jpg" || result.PosterURL != result.CoverURL {
		t.Fatalf("unexpected cover/poster URLs: %q %q", result.CoverURL, result.PosterURL)
	}
	if len(result.Actresses) != 2 {
		t.Fatalf("Actresses len = %d, want 2", len(result.Actresses))
	}
	if result.Actresses[0].FirstName != "Jane" || result.Actresses[0].LastName != "Doe" {
		t.Fatalf("unexpected first actress: %#v", result.Actresses[0])
	}
	if result.Actresses[1].JapaneseName != "花子" {
		t.Fatalf("unexpected second actress: %#v", result.Actresses[1])
	}
}

func TestParseDetailPage_FallbackDescriptionAndScreenshots(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><head>
<meta property="og:title" content="Fallback Title - JAV321">
<meta name="description" content="short">
<meta property="og:description" content="window.ads should be ignored because it looks like ad markup">
</head><body>
<b>ID</b> : XYZ-999<br>
<b>description</b> : This fallback description is long enough to pass validation.<br>
<b>actor</b> : Jane Doe | 花子<br>
<a href="/snapshot/a"></a>
<a href="/snapshot/b"></a>
</body></html>`))
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}

	result := parseDetailPage(doc, "https://jp.jav321.com/video/xyz999", "XYZ-999")
	if result.Description != "This fallback description is long enough to pass validation." {
		t.Fatalf("Description = %q", result.Description)
	}
	if result.Title != "Fallback Title" {
		t.Fatalf("Title = %q, want Fallback Title", result.Title)
	}
	if len(result.ScreenshotURL) != 2 {
		t.Fatalf("ScreenshotURL len = %d, want 2", len(result.ScreenshotURL))
	}
	if result.ScreenshotURL[0] != "https://jp.jav321.com/snapshot/a" {
		t.Fatalf("unexpected screenshot URL: %q", result.ScreenshotURL[0])
	}
}

func TestHelpers(t *testing.T) {
	if !isUsableDescription("This description is comfortably longer than twenty characters.") {
		t.Fatal("expected usable description")
	}
	if isUsableDescription("window.ads bad") {
		t.Fatal("expected ad-like description to be rejected")
	}
	if got := resolveURL("https://jp.jav321.com/video/abc123", "/images/cover.jpg"); got != "https://jp.jav321.com/images/cover.jpg" {
		t.Fatalf("resolveURL absolute path = %q", got)
	}
	if got := resolveURL("https://jp.jav321.com/video/abc123", "shots/1.jpg"); got != "https://jp.jav321.com/video/shots/1.jpg" {
		t.Fatalf("resolveURL relative path = %q", got)
	}
	if got := extractID("Some release ABC-123 sample"); got != "ABC-123" {
		t.Fatalf("extractID = %q, want ABC-123", got)
	}
	if got := stripTrailingSiteName("Great Movie - JAV321"); got != "Great Movie" {
		t.Fatalf("stripTrailingSiteName = %q, want Great Movie", got)
	}
	if got := normalizeID(" ABC-123 "); got != "abc123" {
		t.Fatalf("normalizeID = %q, want abc123", got)
	}
}
