package mgstage

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScrapeURLV4_StatusCodeErrors tests ScrapeURL with various HTTP status codes
func TestScrapeURLV4_StatusCodeErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"404", 404},
		{"403", 403},
		{"429", 429},
		{"500", 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer ts.Close()

			s := &scraper{
				client:      resty.New().SetBaseURL(ts.URL),
				enabled:     true,
				rateLimiter: ratelimit.NewLimiter(0),
				settings:    models.ScraperSettings{Enabled: true},
			}

			result, err := s.ScrapeURL(context.Background(), ts.URL+"/product/product_detail/SIRO-1234/")
			assert.Nil(t, result)
			assert.Error(t, err)
		})
	}
}

// TestSearchV4_ParseHTML tests parsing an MGStage detail page
func TestSearchV4_ParseHTML(t *testing.T) {
	detailHTML := buildMGStageDetailHTML("SIRO-1234", "Test Movie Title", "2024/01/15", "120 min", "TestMaker", "TestLabel", "TestSeries", "Drama, Romance", "Actress A")

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(detailHTML))
	require.NoError(t, err)

	result, err := s.parseHTML(doc, "https://www.mgstage.com/product/product_detail/SIRO-1234/")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "SIRO-1234", result.ID)
	assert.Equal(t, "Test Movie Title", result.Title)
	assert.Equal(t, "TestMaker", result.Maker)
	assert.Equal(t, "TestLabel", result.Label)
}

// TestCanHandleURLV4 tests CanHandleURL
func TestCanHandleURLV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	tests := []struct {
		url      string
		expected bool
	}{
		{"https://www.mgstage.com/product/product_detail/SIRO-1234/", true},
		{"https://mgstage.com/product/product_detail/SIRO-1234/", true},
		{"https://example.com/product/product_detail/SIRO-1234/", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.expected, s.CanHandleURL(tt.url))
		})
	}
}

// TestExtractIDFromURLV4 tests ExtractIDFromURL
func TestExtractIDFromURLV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	tests := []struct {
		name        string
		url         string
		expectedID  string
		expectError bool
	}{
		{"standard product detail", "https://www.mgstage.com/product/product_detail/SIRO-1234/", "SIRO-1234", false},
		{"invalid URL", "://bad", "", true},
		{"no ID in path", "https://www.mgstage.com/", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := s.ExtractIDFromURL(tt.url)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedID, id)
			}
		})
	}
}

// TestResolveSearchQueryV4 tests ResolveSearchQuery
func TestResolveSearchQueryV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	// MGStage URL input should resolve
	url, ok := s.ResolveSearchQuery("https://www.mgstage.com/product/product_detail/SIRO-1234/")
	assert.True(t, ok)
	assert.NotEmpty(t, url)

	// Plain MGStage-style ID should also resolve
	url, ok = s.ResolveSearchQuery("SIRO-1234")
	assert.True(t, ok)
	assert.Equal(t, "SIRO-1234", url)

	// Non-MGStage format should not resolve
	_, ok = s.ResolveSearchQuery("random-text")
	assert.False(t, ok)
}

// TestNormalizeIDForSearchV4 tests ID normalization for search
func TestNormalizeIDForSearchV4(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ABC-123", "abc123"},
		{"abc-123", "abc123"},
		{"SIRO-1234", "siro1234"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizeIDForSearch(tt.input))
		})
	}
}

// TestMGStageIDsMatchV4 tests ID matching logic
func TestMGStageIDsMatchV4(t *testing.T) {
	tests := []struct {
		a, b      string
		wantMatch bool
	}{
		{"SIRO-1234", "SIRO-1234", true},
		{"SIRO-1234", "siro-1234", true},
		{"SIRO-1234", "SIRO-5678", false},
		{"ABC-123", "abc-123", true},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			assert.Equal(t, tt.wantMatch, mgstageIDsMatch(tt.a, tt.b))
		})
	}
}

func buildMGStageDetailHTML(id, title, date, runtime, maker, label, series, genres, actresses string) string {
	return fmt.Sprintf(`<html>
<head><title>%s</title></head>
<body>
<div class="detail_data">
	<table>
		<tr><th>品番：</th><td>%s</td></tr>
		<tr><th>配信開始日：</th><td>%s</td></tr>
		<tr><th>収録時間：</th><td>%s</td></tr>
		<tr><th>メーカー：</th><td><a>%s</a></td></tr>
		<tr><th>レーベル：</th><td><a>%s</a></td></tr>
		<tr><th>シリーズ：</th><td><a>%s</a></td></tr>
	</table>
</div>
<div class="tag"><a>%s</a></div>
</body>
</html>`, title, id, date, runtime, maker, label, series, genres)
}
