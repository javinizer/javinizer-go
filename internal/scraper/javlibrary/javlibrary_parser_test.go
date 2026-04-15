package javlibrary

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
)

func TestParseDetailPage(t *testing.T) {
	settings := config.ScraperSettings{
		Enabled:  true,
		Language: "en",
		BaseURL:  "https://www.javlibrary.com",
	}
	s := New(settings, &config.ProxyConfig{}, config.FlareSolverrConfig{})

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

	result, err := s.parseDetailPage(html, "IPX-123", "https://www.javlibrary.com/en/?v=abc", "en")
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/en/vl_searchbyid.php":
			if got := r.URL.Query().Get("keyword"); got != "IPX-123" {
				t.Fatalf("keyword = %q, want IPX-123", got)
			}
			_, _ = fmt.Fprint(w, `<html><body><a href="?v=javli43uqe">result</a></body></html>`)
		case "/en/":
			if got := r.URL.Query().Get("v"); got != "javli43uqe" {
				t.Fatalf("detail query v = %q, want javli43uqe", got)
			}
			_, _ = fmt.Fprint(w, `<html><head><title>IPX-123 Search Flow Title - JAVLibrary</title></head><body>
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

	settings := config.ScraperSettings{
		Enabled:  true,
		Language: "en",
		BaseURL:  server.URL,
	}
	s := New(settings, &config.ProxyConfig{}, config.FlareSolverrConfig{})

	result, err := s.Search(context.Background(), "IPX-123")
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
	settings := config.ScraperSettings{
		Enabled:  true,
		Language: "en",
		BaseURL:  "https://www.javlibrary.com",
	}
	s := New(settings, &config.ProxyConfig{}, config.FlareSolverrConfig{})

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
	if _, _, ok := s.ResolveDownloadProxyForHost("c.impact.jp"); !ok {
		t.Fatal("expected c.impact.jp (JavLibrary CDN) to be recognized")
	}
	if _, _, ok := s.ResolveDownloadProxyForHost("img.javlibrary.com"); !ok {
		t.Fatal("expected img.javlibrary.com to be recognized")
	}
	if _, _, ok := s.ResolveDownloadProxyForHost("example.com"); ok {
		t.Fatal("expected example.com to NOT be recognized")
	}
}

func TestExtractDescription(t *testing.T) {
	settings := config.ScraperSettings{
		Enabled:  true,
		Language: "en",
		BaseURL:  "https://www.javlibrary.com",
	}
	s := New(settings, &config.ProxyConfig{}, config.FlareSolverrConfig{})

	// Test meta description tag (primary method)
	html := `<html><head><meta name="description" content="This is a great movie with excellent quality!"></head><body><div id="video_review"><div class="text">This is a great movie with excellent quality!</div></div></body></html>`
	if got := s.extractDescription(html); got != "This is a great movie with excellent quality!" {
		t.Fatalf("extractDescription = %q, want 'This is a great movie...'", got)
	}

	// Test fallback to video_review div text (when no meta tag)
	// JavLibrary uses table structure in video_review
	html = `<html><head></head><body><div id="video_review"><table><tr><td class="text">Sample review text here</td></tr></table></div></body></html>`
	if got := s.extractDescription(html); got != "Sample review text here" {
		t.Fatalf("extractDescription fallback = %q", got)
	}

	// Empty test - no meta description, no review text
	html = `<html><head></head><body><div>no review</div></body></html>`
	if got := s.extractDescription(html); got != "" {
		t.Fatalf("extractDescription empty = %q, want empty", got)
	}
}

func TestExtractSeries(t *testing.T) {
	settings := config.ScraperSettings{
		Enabled:  true,
		Language: "en",
		BaseURL:  "https://www.javlibrary.com",
	}
	s := New(settings, &config.ProxyConfig{}, config.FlareSolverrConfig{})

	html := `<div id="video_series"><a href="/series/test">Test Series Name</a></div>`
	if got := s.extractSeries(html); got != "Test Series Name" {
		t.Fatalf("extractSeries = %q", got)
	}

	// Fallback test
	html = `<div>Series: <a href="/series/abc">ABC Series</a></div>`
	if got := s.extractSeries(html); got != "ABC Series" {
		t.Fatalf("extractSeries fallback = %q", got)
	}

	// Empty test
	html = `<div>no series</div>`
	if got := s.extractSeries(html); got != "" {
		t.Fatalf("extractSeries empty = %q, want empty", got)
	}
}

func TestExtractRating(t *testing.T) {
	settings := config.ScraperSettings{
		Enabled:  true,
		Language: "en",
		BaseURL:  "https://www.javlibrary.com",
	}
	s := New(settings, &config.ProxyConfig{}, config.FlareSolverrConfig{})

	// Test standard rating format
	html := `<div id="video_rating"><span class="num">4.5</span> / 5.0</div>`
	if got := s.extractRating(html); got == nil || got.Score != 4.5 {
		t.Fatalf("extractRating = %v, want 4.5", got)
	}

	// Test fallback format (4.5 out of 5)
	html = `<div>Rating: 4.0 out of 5</div>`
	if got := s.extractRating(html); got == nil || got.Score != 4.0 {
		t.Fatalf("extractRating fallback = %v, want 4.0", got)
	}

	// No rating test
	html = `<div>no rating</div>`
	if got := s.extractRating(html); got != nil {
		t.Fatalf("extractRating empty = %v, want nil", got)
	}
}

func TestExtractScreenshotURLs(t *testing.T) {
	settings := config.ScraperSettings{
		Enabled:  true,
		Language: "en",
		BaseURL:  "https://www.javlibrary.com",
	}
	s := New(settings, &config.ProxyConfig{}, config.FlareSolverrConfig{})

	html := `<img src="https://example.com/pic01.jpg"><img src="https://example.com/pic02.jpg">`
	got := s.extractScreenshotURLs(html)
	if len(got) != 2 {
		t.Fatalf("extractScreenshotURLs count = %d, want 2", len(got))
	}

	// Test with sample movie images
	html = `<img src="https://example.com/sample_movie.jpg">`
	got = s.extractScreenshotURLs(html)
	if len(got) != 1 || got[0] != "https://example.com/sample_movie.jpg" {
		t.Fatalf("extractScreenshotURLs sample = %v", got)
	}

	// Empty test
	html = `<div>no images</div>`
	got = s.extractScreenshotURLs(html)
	if len(got) != 0 {
		t.Fatalf("extractScreenshotURLs empty = %v, want empty", got)
	}
}

func TestExtractScreenshotURLs_DMMFiltering(t *testing.T) {
	settings := config.ScraperSettings{
		Enabled:  true,
		Language: "en",
		BaseURL:  "https://www.javlibrary.com",
	}
	s := New(settings, &config.ProxyConfig{}, config.FlareSolverrConfig{})

	tests := []struct {
		name    string
		html    string
		wantLen int
	}{
		{
			name:    "filter pl.jpg cover URL",
			html:    `<img src="https://images.example.com/ipx123pl.jpg">`,
			wantLen: 0,
		},
		{
			name:    "filter ps.jpg poster URL",
			html:    `<img src="https://images.example.com/ipx123ps.jpg">`,
			wantLen: 0,
		},
		{
			name:    "filter pl.jpg in path",
			html:    `<img src="https://c.impact.jp/abc123/special/pl.jpg">`,
			wantLen: 0,
		},
		{
			name:    "numeric pattern /04.jpg passes filter",
			html:    `<img src="https://c.impact.jp/abc123/04.jpg">`,
			wantLen: 1,
		},
		{
			name:    "numeric pattern /001.jpg passes filter",
			html:    `<img src="https://c.impact.jp/abc123/001.jpg">`,
			wantLen: 1,
		},
		{
			name:    "numeric pattern in href passes filter",
			html:    `<a href="https://c.impact.jp/abc123/04.jpg">screenshot</a>`,
			wantLen: 1,
		},
		{
			name:    "non-numeric jpg in src is filtered by stricter regex",
			html:    `<img src="https://example.com/numericless.jpg">`,
			wantLen: 0,
		},
		{
			name:    "non-numeric jpg in href is filtered by stricter regex",
			html:    `<a href="https://example.com/numericless.jpg">screenshot</a>`,
			wantLen: 0,
		},
		{
			name:    "mix of valid screenshots and filtered covers",
			html:    `<img src="https://c.impact.jp/abc/04.jpg"><img src="https://c.impact.jp/abc/pl.jpg"><img src="https://c.impact.jp/abc/05.jpg">`,
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.extractScreenshotURLs(tt.html)
			if len(got) != tt.wantLen {
				t.Errorf("extractScreenshotURLs() len = %d, want %d, got URLs: %v", len(got), tt.wantLen, got)
			}
		})
	}
}

func TestExtractTrailerURL(t *testing.T) {
	settings := config.ScraperSettings{
		Enabled:  true,
		Language: "en",
		BaseURL:  "https://www.javlibrary.com",
	}
	s := New(settings, &config.ProxyConfig{}, config.FlareSolverrConfig{})

	// Test mp4 URL
	html := `<a href="https://example.com/sample_movie.mp4">sample</a>`
	if got := s.extractTrailerURL(html); got != "https://example.com/sample_movie.mp4" {
		t.Fatalf("extractTrailerURL = %q", got)
	}

	// Empty test
	html = `<div>no trailer</div>`
	if got := s.extractTrailerURL(html); got != "" {
		t.Fatalf("extractTrailerURL empty = %q, want empty", got)
	}
}

func TestParseDetailPage_FullData(t *testing.T) {
	settings := config.ScraperSettings{
		Enabled:  true,
		Language: "en",
		BaseURL:  "https://www.javlibrary.com",
	}
	s := New(settings, &config.ProxyConfig{}, config.FlareSolverrConfig{})

	html := `<html><head><title>IPX-123 Full Data Test - JAVLibrary</title></head><body>
<div id="video_info"></div>
<img id="video_jacket_img" src="https://images.example.com/ipx123pl.jpg">
<div id="video_date"><td class="text">2026-02-16</td></div>
<div id="video_length"><span class="text">125</span> min</div>
<div id="video_director"><a href="/director/test">Director Test</a></div>
<div id="video_maker"><a href="/maker/test">Maker Test</a></div>
<div id="video_label"><a href="/label/test">Label Test</a></div>
<div id="video_series"><a href="/series/test">Series Test</a></div>
<div id="video_review"><table><tr><td class="text">This is a great movie review!</td></tr></table></div>
<div id="video_rating"><span class="num">4.5</span> / 5.0</div>
<span class="genre"><a href="/genres/drama">Drama</a></span>
<span class="star"><a href="/star/jane">Jane Doe</a></span>
<img src="https://images.example.com/sample1.jpg">
<img src="https://images.example.com/sample2.jpg">
<a href="https://example.com/trailer.mp4">trailer</a>
</body></html>`

	result, err := s.parseDetailPage(html, "IPX-123", "https://www.javlibrary.com/en/?v=test", "en")
	if err != nil {
		t.Fatalf("parseDetailPage returned error: %v", err)
	}

	// Verify new fields
	if result.Description != "This is a great movie review!" {
		t.Fatalf("Description = %q, want 'This is a great movie review!'", result.Description)
	}
	if result.Series != "Series Test" {
		t.Fatalf("Series = %q", result.Series)
	}
	if result.Rating == nil || result.Rating.Score != 4.5 {
		t.Fatalf("Rating = %v, want 4.5", result.Rating)
	}
	if len(result.ScreenshotURL) != 2 {
		t.Fatalf("ScreenshotURL count = %d, want 2", len(result.ScreenshotURL))
	}
	if result.TrailerURL != "https://example.com/trailer.mp4" {
		t.Fatalf("TrailerURL = %q", result.TrailerURL)
	}
}
