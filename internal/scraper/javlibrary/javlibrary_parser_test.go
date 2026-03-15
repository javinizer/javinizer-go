package javlibrary

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
)

func TestParseDetailPage(t *testing.T) {
	s, err := New(&config.JavLibraryConfig{
		Enabled:  true,
		Language: "en",
		BaseURL:  "https://www.javlibrary.com",
	}, &config.ProxyConfig{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	html := `<html><head><title>IPX-123 Sample Title - JAVLibrary</title></head><body>
<div id="video_info"></div>
<img id="video_jacket_img" src="https://images.example.com/ipx123pl.jpg">
<div id="video_date"><td class="text">2026-02-16</td></div>
<div id="video_length"><span class="text">125</span> min</div>
<div id="video_director"><a href="/director/test">Director Test</a></div>
<div id="video_maker"><a href="/maker/test">Maker Test</a></div>
<div id="video_label"><a href="/label/test">Label Test</a></div>
<span class="genre"><a href="/genres/drama">Drama</a></span>
<span class="genre"><a href="/genres/romance">Romance</a></span>
<span class="genre"><a href="/genres/drama">Drama</a></span>
<span class="star"><a href="/star/jane">Jane Doe</a></span>
<span class="star"><a href="/star/mary">Mary Major</a></span>
</body></html>`

	result, err := s.parseDetailPage(html, "IPX-123", "https://www.javlibrary.com/en/?v=abc")
	if err != nil {
		t.Fatalf("parseDetailPage returned error: %v", err)
	}

	if result.Source != "javlibrary" {
		t.Fatalf("Source = %q, want javlibrary", result.Source)
	}
	if result.SourceURL != "https://www.javlibrary.com/en/?v=abc" {
		t.Fatalf("SourceURL = %q", result.SourceURL)
	}
	if result.ID != "IPX-123" {
		t.Fatalf("ID = %q, want IPX-123", result.ID)
	}
	if result.Title != "Sample Title" {
		t.Fatalf("Title = %q, want Sample Title", result.Title)
	}
	if result.CoverURL != "https://images.example.com/ipx123pl.jpg" {
		t.Fatalf("CoverURL = %q", result.CoverURL)
	}
	if result.PosterURL != "https://images.example.com/ipx123ps.jpg" {
		t.Fatalf("PosterURL = %q", result.PosterURL)
	}
	if result.Runtime != 125 {
		t.Fatalf("Runtime = %d, want 125", result.Runtime)
	}
	if result.Director != "Director Test" || result.Maker != "Maker Test" || result.Label != "Label Test" {
		t.Fatalf("unexpected fields: director=%q maker=%q label=%q", result.Director, result.Maker, result.Label)
	}
	if len(result.Genres) != 2 || result.Genres[0] != "Drama" || result.Genres[1] != "Romance" {
		t.Fatalf("Genres = %#v", result.Genres)
	}
	if len(result.Actresses) != 2 || result.Actresses[0].FirstName != "Jane" || result.Actresses[0].LastName != "Doe" {
		t.Fatalf("Actresses = %#v", result.Actresses)
	}
	wantDate := time.Date(2026, 2, 16, 0, 0, 0, 0, time.UTC)
	if result.ReleaseDate == nil || !result.ReleaseDate.Equal(wantDate) {
		t.Fatalf("ReleaseDate = %v, want %v", result.ReleaseDate, wantDate)
	}
}

func TestSearch_SearchResultFlow(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/en/vl_searchbyid.php":
			if got := r.URL.Query().Get("keyword"); got != "IPX-123" {
				t.Fatalf("keyword = %q, want IPX-123", got)
			}
			fmt.Fprint(w, `<html><body><a href="?v=javli43uqe">result</a></body></html>`)
		case "/en/":
			if got := r.URL.Query().Get("v"); got != "javli43uqe" {
				t.Fatalf("detail query v = %q, want javli43uqe", got)
			}
			fmt.Fprint(w, `<html><head><title>IPX-123 Search Flow Title - JAVLibrary</title></head><body>
<div id="video_info"></div>
<a id="video_jacket" href="//images.example.com/ipx123pl.jpg"></a>
<div id="video_date"><span class="text">2026-02-17</span></div>
<div id="video_length"><span class="text">90</span></div>
<div id="video_maker"><a href="/maker/test">Maker Test</a></div>
<span class="genre"><a href="/genres/drama">Drama</a></span>
<span class="star"><a href="/star/jane">Jane Doe</a></span>
</body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s, err := New(&config.JavLibraryConfig{
		Enabled:  true,
		Language: "en",
		BaseURL:  server.URL,
	}, &config.ProxyConfig{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	result, err := s.Search("IPX-123")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if result.SourceURL != server.URL+"/en/?v=javli43uqe" {
		t.Fatalf("SourceURL = %q", result.SourceURL)
	}
	if result.Title != "Search Flow Title" {
		t.Fatalf("Title = %q", result.Title)
	}
	if result.CoverURL != "https://images.example.com/ipx123pl.jpg" || result.PosterURL != "https://images.example.com/ipx123ps.jpg" {
		t.Fatalf("unexpected cover URLs: %q %q", result.CoverURL, result.PosterURL)
	}
}

func TestHelpers(t *testing.T) {
	s, err := New(&config.JavLibraryConfig{
		Enabled:  true,
		Language: "en",
		BaseURL:  "https://www.javlibrary.com",
	}, &config.ProxyConfig{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if got := s.extractMovieURLFromHTML(`<a href="/en/?v=javli43uqe">match</a>`); got != "/en/?v=javli43uqe" {
		t.Fatalf("extractMovieURLFromHTML absolute = %q", got)
	}
	if got := s.extractMovieURLFromHTML(`<a href="?v=javli43uqe">match</a>`); got != "?v=javli43uqe" {
		t.Fatalf("extractMovieURLFromHTML relative = %q", got)
	}
	if got := s.extractReleaseDate(`Release Date: 2026-02-18`); got == nil || !got.Equal(time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("extractReleaseDate fallback = %v", got)
	}
	if got := s.extractRuntime(`Duration: 105 min`); got != 105 {
		t.Fatalf("extractRuntime fallback = %d", got)
	}
	if got := s.extractField(`<div id="video_director"><a>Director</a></div>`, "video_director"); got != "Director" {
		t.Fatalf("extractField = %q", got)
	}
	if _, _, ok := s.ResolveDownloadProxyForHost("www.javlibrary.com"); !ok {
		t.Fatal("expected JavLibrary host to be recognized")
	}
}
