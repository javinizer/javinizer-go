package javbus

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

func newJavbusTestScraper(baseURL string) *Scraper {
	cfg := config.DefaultConfig()
	cfg.Scrapers.JavBus.Enabled = true
	cfg.Scrapers.JavBus.BaseURL = baseURL
	cfg.Scrapers.JavBus.Language = "ja"
	cfg.Scrapers.JavBus.RequestDelay = 0
	return New(cfg)
}

func TestScraperGetURLAndSearch(t *testing.T) {
	var searchRequests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		searchRequests = append(searchRequests, r.URL.Path)
		switch r.URL.Path {
		case "/search/ABC-123&type=0&parent=uc":
			_, _ = fmt.Fprint(w, `<html><body><a class="movie-box" href="/ABC-123" title="ABC-123 Example Title"><date>ABC-123</date></a></body></html>`)
		case "/ja/ABC-123":
			_, _ = fmt.Fprint(w, `<html>
<head>
  <title>ABC-123 Example Title - JavBus</title>
  <meta name="description" content="Example description">
</head>
<body>
  <div id="info">
    <p><span class="header">発売日:</span> 2024-01-20</p>
    <p><span class="header">収録時間:</span> 120分</p>
    <p><span class="header">監督:</span><a>Jane Director</a></p>
    <p><span class="header">メーカー:</span><a>IdeaPocket</a></p>
    <p><span class="header">レーベル:</span><a>Premium</a></p>
    <p><span class="header">シリーズ:</span><a>Flagship</a></p>
  </div>
  <div id="genre-toggle"><a>Drama</a><a>Romance</a></div>
  <div id="star-div">
    <a href="/star/abc"><img title="河合あすな" src="https://img.example/star.jpg"></a>
  </div>
  <a class="bigImage" href="/pics/cover/abc_b.jpg"><img src="/pics/cover/abc_s.jpg"></a>
  <div id="sample-waterfall">
    <a class="sample-box" href="https://pics.dmm.co.jp/digital/video/abc123/abc123-1.jpg"></a>
  </div>
  <video><source src="https://cdn.example/trailer.mp4"></video>
</body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	scraper := newJavbusTestScraper(server.URL)

	url, err := scraper.GetURL("ABC-123")
	if err != nil {
		t.Fatalf("GetURL() error = %v", err)
	}
	if got, want := url, server.URL+"/ja/ABC-123"; got != want {
		t.Fatalf("GetURL() = %q, want %q", got, want)
	}

	result, err := scraper.Search("ABC-123")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if result.ID != "ABC-123" {
		t.Fatalf("result.ID = %q", result.ID)
	}
	if result.Title != "Example Title" {
		t.Fatalf("result.Title = %q", result.Title)
	}
	if result.Description != "Example description" {
		t.Fatalf("result.Description = %q", result.Description)
	}
	if result.Maker != "IdeaPocket" || result.Label != "Premium" || result.Series != "Flagship" || result.Director != "Jane Director" {
		t.Fatalf("unexpected info link extraction: %#v", result)
	}
	if len(result.Actresses) != 1 || result.Actresses[0].JapaneseName != "河合あすな" {
		t.Fatalf("Actresses = %#v", result.Actresses)
	}
	if len(result.Genres) != 2 || result.Genres[0] != "Drama" || result.Genres[1] != "Romance" {
		t.Fatalf("Genres = %#v", result.Genres)
	}
	if result.CoverURL != server.URL+"/pics/cover/abc_b.jpg" {
		t.Fatalf("CoverURL = %q", result.CoverURL)
	}
	if len(result.ScreenshotURL) != 1 || result.ScreenshotURL[0] != "https://pics.dmm.co.jp/digital/video/abc123/abc123jp-1.jpg" {
		t.Fatalf("ScreenshotURL = %#v", result.ScreenshotURL)
	}
	if result.TrailerURL != "https://cdn.example/trailer.mp4" {
		t.Fatalf("TrailerURL = %q", result.TrailerURL)
	}
	if len(searchRequests) < 2 {
		t.Fatalf("expected search and detail requests, got %#v", searchRequests)
	}
}

func TestScraperGetURLAcceptsDirectHTTPURL(t *testing.T) {
	scraper := newJavbusTestScraper("https://www.javbus.com")

	got, err := scraper.GetURL("https://www.javbus.com/ABC-123")
	if err != nil {
		t.Fatalf("GetURL() error = %v", err)
	}
	if got != "https://www.javbus.com/ja/ABC-123" {
		t.Fatalf("GetURL() = %q", got)
	}
}

func TestScraperGetURLReturnsChallengeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `<html><head><title>Age Verification JavBus - JavBus</title></head></html>`)
	}))
	defer server.Close()

	scraper := newJavbusTestScraper(server.URL)
	_, err := scraper.GetURL("ABC-123")
	if err == nil {
		t.Fatal("expected challenge error")
	}
	if scraperErr, ok := models.AsScraperError(err); !ok || scraperErr.Kind != models.ScraperErrorKindBlocked {
		t.Fatalf("expected blocked scraper error, got %#v", err)
	}
}

func TestScraperSearchReturnsNotFoundAndDisabledErrors(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `<html><body>No results</body></html>`)
		}))
		defer server.Close()

		scraper := newJavbusTestScraper(server.URL)
		_, err := scraper.GetURL("ABC-123")
		if err == nil {
			t.Fatal("expected not found error")
		}
		if scraperErr, ok := models.AsScraperError(err); !ok || scraperErr.Kind != models.ScraperErrorKindNotFound {
			t.Fatalf("expected not found scraper error, got %#v", err)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Scrapers.JavBus.Enabled = false
		scraper := New(cfg)
		_, err := scraper.Search("ABC-123")
		if err == nil || !strings.Contains(err.Error(), "disabled") {
			t.Fatalf("expected disabled error, got %v", err)
		}
	})
}
