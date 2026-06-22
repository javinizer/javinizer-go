package javlibrary

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildJavLibDetailHTML creates a JavLibrary detail page
func buildJavLibDetailHTML(id, title, date, runtime, maker, label, director, series, genres, actresses, coverURL string) string {
	genreHTML := ""
	for _, g := range strings.Split(genres, ",") {
		g = strings.TrimSpace(g)
		if g != "" {
			genreHTML += fmt.Sprintf(`<a href="/genre/1">%s</a>`, g)
		}
	}

	actressHTML := ""
	for _, a := range strings.Split(actresses, ",") {
		a = strings.TrimSpace(a)
		if a != "" {
			actressHTML += fmt.Sprintf(`<a href="/star/1">%s</a>`, a)
		}
	}

	return fmt.Sprintf(`<html>
<head><title>%s %s - JAVLibrary</title></head>
<body>
<div id="video_info">
	<div id="video_id"><td class="text">%s</td></div>
	<div id="video_date"><td class="text">%s</td></div>
	<div id="video_length"><td class="text">%s</td></div>
	<div id="video_maker"><td class="text"><a>%s</a></td></div>
	<div id="video_label"><td class="text"><a>%s</a></td></div>
	<div id="video_director"><td class="text"><a>%s</a></td></div>
	<div id="video_genres">%s</div>
	<div id="video_cast">%s</div>
	<div id="video_cover"><img src="%s"/></div>
</div>
</body>
</html>`, id, title, id, date, runtime, maker, label, director, genreHTML, actressHTML, coverURL)
}

// TestSearchV5_Disabled tests Search when disabled
func TestSearchV5_Disabled(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.javlibrary.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	result, err := s.Search(context.Background(), "IPX-999")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestSearchV5_FullE2E tests Search with httptest server
func TestSearchV5_FullE2E(t *testing.T) {
	detailHTML := buildJavLibDetailHTML("IPX-999", "Test Movie", "2024-01-15", "120", "TestMaker", "TestLabel", "TestDirector", "TestSeries", "Action, Drama", "Actress A", "https://pics.dmm.co.jp/cover.jpg")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		if strings.Contains(r.URL.Path, "vl_searchbyid") {
			// Return detail page directly (JavLibrary sometimes does this)
			fmt.Fprint(w, detailHTML)
			return
		}
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.Search(context.Background(), "IPX-999")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-999", result.ID)
}

// TestScrapeURLV5_NonJavLibURL tests ScrapeURL rejecting non-JavLibrary URLs
func TestScrapeURLV5_NonJavLibURL(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.javlibrary.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.ScrapeURL(context.Background(), "https://example.com/?v=IPX-999")
	assert.Nil(t, result)
	assert.Error(t, err)
}

// TestGetURLV5 tests GetURL
func TestGetURLV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		baseURL:  "https://www.javlibrary.com",
		language: "ja",
		settings: models.ScraperSettings{Enabled: true},
	}

	url, err := s.GetURL(context.Background(), "IPX-999")
	require.NoError(t, err)
	assert.Contains(t, url, "vl_searchbyid")
	assert.Contains(t, url, "IPX-999")
}

// TestCanHandleURLV5 tests CanHandleURL
func TestCanHandleURLV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		baseURL:  "https://www.javlibrary.com",
		settings: models.ScraperSettings{Enabled: true},
	}

	tests := []struct {
		url      string
		expected bool
	}{
		{"https://www.javlibrary.com/ja/?v=IPX-999", true},
		{"https://javlibrary.com/en/?v=IPX-999", true},
		{"https://example.com/?v=IPX-999", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.expected, s.CanHandleURL(tt.url))
		})
	}
}

// TestExtractIDFromURLV5 tests URL ID extraction
func TestExtractIDFromURLV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	tests := []struct {
		name        string
		url         string
		expected    string
		expectError bool
	}{
		{"with v param", "https://www.javlibrary.com/ja/?v=IPX-999", "IPX-999", false},
		{"no v param", "https://www.javlibrary.com/ja/", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := s.ExtractIDFromURL(tt.url)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, id)
			}
		})
	}
}

// TestNewScraperV5 tests scraper creation
func TestNewScraperV5(t *testing.T) {
	settings := &models.ScraperSettings{
		Enabled: true,
		BaseURL: "https://www.javlibrary.com",
	}

	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	require.NotNil(t, s)
	assert.True(t, s.enabled)
}

// TestNewScraperV5_DefaultBaseURL tests default base URL
func TestNewScraperV5_DefaultBaseURL(t *testing.T) {
	settings := &models.ScraperSettings{
		Enabled: true,
		BaseURL: "",
	}

	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	require.NotNil(t, s)
	assert.Equal(t, "http://www.javlibrary.com", s.baseURL)
}

// TestParseDetailPageV5 tests detail page parsing
func TestParseDetailPageV5(t *testing.T) {
	html := buildJavLibDetailHTML("IPX-001", "Test Movie", "2024-01-15", "120", "MakerA", "LabelB", "DirC", "SeriesD", "Action", "Actress A", "https://pics.dmm.co.jp/cover.jpg")

	s := &scraper{
		enabled:  true,
		language: "ja",
		settings: models.ScraperSettings{Enabled: true},
	}

	result, err := s.parseDetailPage(html, "IPX-001", "https://www.javlibrary.com/ja/?v=IPX-001", "ja")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-001", result.ID)
}

// TestSearchV5_NotFound tests Search when movie is not found
func TestSearchV5_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, `<html><body>No results found</body></html>`)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.Search(context.Background(), "NOTFOUND-999")
	assert.Nil(t, result)
	assert.Error(t, err)
}

// TestExtractMovieURLFromHTMLV5 tests extracting movie URL from search results
func TestExtractMovieURLFromHTMLV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		language: "ja",
		settings: models.ScraperSettings{Enabled: true},
	}

	html := `<div class="video" id="vid_javliat76u"><div class="id">IPX-999</div></div>`

	result := s.extractMovieURLFromHTML(html, "IPX-999")
	assert.Contains(t, result, "?v=javliat76u")
}

// TestIsValidLanguageV5 tests language validation
func TestIsValidLanguageV5(t *testing.T) {
	assert.True(t, isValidLanguage("ja"))
	assert.True(t, isValidLanguage("en"))
	assert.True(t, isValidLanguage("cn"))
	assert.True(t, isValidLanguage("tw"))
	assert.False(t, isValidLanguage("zh"))
	assert.False(t, isValidLanguage("fr"))
	assert.False(t, isValidLanguage(""))
}
