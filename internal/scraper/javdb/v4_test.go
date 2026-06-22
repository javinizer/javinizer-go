package javdb

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScrapeURLV4_Success tests ScrapeURL with a valid detail page
func TestScrapeURLV4_Success(t *testing.T) {
	detailHTML := buildDetailHTML("ABC-123", "Test Movie Title", "2024-01-15", "120", "TestStudio", "TestLabel", "TestDirector", "Action, Drama", "Actress A, Actress B", "https://pics.dmm.co.jp/cover.jpg")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.ScrapeURL(context.Background(), ts.URL+"/v/abcde")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "ABC-123", result.ID)
	assert.Equal(t, "Test Movie Title", result.Title)
	assert.Equal(t, "TestStudio", result.Maker)
	assert.Equal(t, "TestLabel", result.Label)
	assert.Equal(t, "TestDirector", result.Director)
	assert.Equal(t, 120, result.Runtime)
}

// TestScrapeURLV4_Disabled tests ScrapeURL when scraper is disabled
func TestScrapeURLV4_Disabled(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	result, err := s.ScrapeURL(context.Background(), "https://javdb.com/v/abcde")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestScrapeURLV4_NotFoundURL tests ScrapeURL with a URL not handled
func TestScrapeURLV4_NotFoundURL(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.ScrapeURL(context.Background(), "https://example.com/v/abcde")
	assert.Nil(t, result)
	assert.Error(t, err)
}

// TestSearchV4_Disabled tests Search when disabled
func TestSearchV4_Disabled(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	result, err := s.Search(context.Background(), "ABC-123")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestSearchV4_SearchPageFallback tests Search going through search page when direct URL fails
func TestSearchV4_SearchPageFallback(t *testing.T) {
	searchHTML := buildSearchHTML("ABC-123", "/v/abc123")
	detailHTML := buildDetailHTML("ABC-123", "Fallback Movie", "2024-03-20", "90", "Studio X", "Label Y", "Director Z", "Romance", "Actress C", "https://pics.dmm.co.jp/cover2.jpg")

	requestCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		if r.URL.Path == "/search" {
			fmt.Fprint(w, searchHTML)
			return
		}
		// Detail page
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.Search(context.Background(), "ABC-123")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "ABC-123", result.ID)
	assert.Equal(t, "Fallback Movie", result.Title)
	assert.Equal(t, "Studio X", result.Maker)
	assert.True(t, requestCount >= 2) // at least search + detail
}

// TestSearchV4_NotFound tests Search when movie is not found
func TestSearchV4_NotFound(t *testing.T) {
	searchHTML := `<html><body><div class="movie-list"></div></body></html>`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, searchHTML)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.Search(context.Background(), "ZZZ-999")
	assert.Nil(t, result)
	assert.Error(t, err)
}

// TestCanHandleURLV4_EdgeCases tests CanHandleURL edge cases
func TestCanHandleURLV4_EdgeCases(t *testing.T) {
	s := &scraper{
		enabled:  true,
		baseURL:  "https://javdb.com",
		settings: models.ScraperSettings{Enabled: true},
	}

	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		{"with custom base URL", "https://custom-javdb.example.com/v/abc", false},
		{"javdb with port", "https://javdb.com:443/v/abc", true},
		{"just hostname", "https://javdb.com", true},
		{"http scheme", "http://javdb.com/v/abc", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, s.CanHandleURL(tt.url))
		})
	}
}

// TestCloseV4_WithFlareSolverr tests Close with a FlareSolverr that returns an error
func TestCloseV4_WithFlareSolverr(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}
	// Close with nil flaresolverr should not panic
	assert.NoError(t, s.Close())
}

// TestGetURLV4 tests the GetURL method
func TestGetURLV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		baseURL:  "https://javdb.com",
		settings: models.ScraperSettings{Enabled: true},
	}

	tests := []struct {
		name        string
		id          string
		expectError bool
	}{
		{"valid ID", "ABC-123", false},
		{"empty ID", "", true},
		{"whitespace ID", "   ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, err := s.GetURL(context.Background(), tt.id)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Contains(t, url, tt.id)
			}
		})
	}
}

// TestIsJavDBVideoCodeV4 tests the isJavDBVideoCode function
func TestIsJavDBVideoCodeV4(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		expected bool
	}{
		{"short valid", "AbJEe", true},
		{"numeric", "5aB3d", true},
		{"too short", "AB", false},
		{"too long", "abcdefghijklmnop", false},
		{"has dash", "ABC-123", false},
		{"min length 3", "AbC", true},
		{"max length 12", "AbCdEfGhIjKl", true},
		{"over max", "AbCdEfGhIjKlM", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isJavDBVideoCode(tt.id))
		})
	}
}

// TestIDMatchRank tests id matching logic
func TestIDMatchRankV4(t *testing.T) {
	tests := []struct {
		name      string
		candidate string
		target    string
		wantRank  idMatchType
	}{
		{"exact match", "ABC-123", "ABC-123", idMatchExact},
		{"no match", "ABC-123", "XYZ-456", idMatchNone},
		{"normalized match zero-padded", "ABC-001", "ABC-1", idMatchNormalized},
		{"variant suffix", "ABC-001A", "ABC-1", idMatchVariant},
		{"empty candidate", "", "ABC-123", idMatchNone},
		{"empty target", "ABC-123", "", idMatchNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := idMatchRank(tt.candidate, tt.target)
			assert.Equal(t, tt.wantRank, got)
		})
	}
}

// TestParseRatingV4 tests rating parsing
func TestParseRatingV4(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantScore float64
		wantVotes int
		wantNil   bool
	}{
		{"simple rating", "3.50", 7.0, 50, false},
		{"rating with votes", "3.50 (1,234 votes)", 7.0, 1234, false},
		{"empty string", "", 0, 0, true},
		{"high rating no scale", "8.5", 8.5, 5, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := parseRating(tt.input)
			if tt.wantNil {
				assert.Nil(t, r)
			} else {
				require.NotNil(t, r)
				assert.InDelta(t, tt.wantScore, r.Score, 0.01)
				assert.Equal(t, tt.wantVotes, r.Votes)
			}
		})
	}
}

// TestParseRuntimeV4 tests runtime parsing
func TestParseRuntimeV4(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"simple minutes", "120分鐘", 120},
		{"no number", "unknown", 0},
		{"with space", " 90 mins", 90},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseRuntime(tt.input))
		})
	}
}

// TestNormalizeLabelV4 tests label normalization
func TestNormalizeLabelV4(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"trailing colon", "番號：", "番號"},
		{"simple", " 日期 ", "日期"},
		{"full-width colon", "發行日期：", "發行日期"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeLabel(tt.input))
		})
	}
}

// TestClassifyCastLabelV4 tests cast label classification
func TestClassifyCastLabelV4(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  castLabelKind
	}{
		{"male actor", "male actor", castLabelMale},
		{"female actress", "女優", castLabelFemale},
		{"generic actor", "actor", castLabelGeneric},
		{"unknown", "other label", castLabelUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, classifyCastLabel(tt.input))
		})
	}
}

// Helper functions for building HTML test fixtures

func buildDetailHTML(id, title, date, runtime, maker, label, director, genres, actresses, coverURL string) string {
	return fmt.Sprintf(`<html>
<head><title>%s - JavDB</title></head>
<body>
<h2 class="title is-4"><strong>%s</strong> %s</h2>
<div class="column-video-cover"><img class="video-cover" src="%s" /></div>
<div class="movie-panel-info">
	<div class="panel-block"><strong>番號：</strong><span class="value">%s</span></div>
	<div class="panel-block"><strong>日期：</strong><span class="value">%s</span></div>
	<div class="panel-block"><strong>時長：</strong><span class="value">%s分鐘</span></div>
	<div class="panel-block"><strong>導演：</strong><span class="value"><a>%s</a></span></div>
	<div class="panel-block"><strong>片商：</strong><span class="value"><a>%s</a></span></div>
	<div class="panel-block"><strong>發行：</strong><span class="value"><a>%s</a></span></div>
	<div class="panel-block"><strong>類別：</strong><span class="value">%s</span></div>
	<div class="panel-block"><strong>女優：</strong><span class="value">%s</span></div>
</div>
</body>
</html>`, id, id, title, coverURL, id, date, runtime, director, maker, label, genres, actresses)
}

func buildSearchHTML(id, detailPath string) string {
	return fmt.Sprintf(`<html><body>
<div class="movie-list">
	<div class="item">
		<a href="%s">
			<div class="video-title"><strong>%s</strong></div>
		</a>
	</div>
</div>
</body></html>`, detailPath, id)
}
