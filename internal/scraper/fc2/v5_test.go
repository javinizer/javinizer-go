package fc2

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildFC2DetailHTML creates a complete FC2 detail page
func buildFC2DetailHTMLV5(articleID, title, date, runtime, maker, genres, actresses, coverURL string) string {
	genreHTML := ""
	for _, g := range strings.Split(genres, ",") {
		g = strings.TrimSpace(g)
		if g != "" {
			genreHTML += fmt.Sprintf(`<a href="/genre/%s">%s</a>`, g, g)
		}
	}

	actressHTML := ""
	for _, a := range strings.Split(actresses, ",") {
		a = strings.TrimSpace(a)
		if a != "" {
			actressHTML += fmt.Sprintf(`<a href="/actor/%s">%s</a>`, a, a)
		}
	}

	return fmt.Sprintf(`<html>
<head>
<meta property="og:title" content="%s"/>
<meta property="og:image" content="%s"/>
<script>var data = {"product_id": "FC2 PPV %s"};</script>
</head>
<body>
<div class="items_article_MainitemThumb">
	<img src="%s" alt="cover"/>
</div>
<div class="items_article_Contents">
	<section class="items_article_Releaseinfo">
		<p class="items_article_Releaseinfo_Date">%s</p>
	</section>
	<section>
		<dl>
			<dt>販売者</dt><dd><a>%s</a></dd>
			<dt>再生時間</dt><dd>%s</dd>
		</dl>
	</section>
	<section>
		<div class="items_article_TagArea">%s</div>
	</section>
	<section class="items_article_Actor">%s</section>
</div>
</body>
</html>`, title, coverURL, articleID, coverURL, date, maker, runtime, genreHTML, actressHTML)
}

// TestSearchV5_FullE2E tests Search with httptest server
func TestSearchV5_FullE2E(t *testing.T) {
	detailHTML := buildFC2DetailHTMLV5("1234567", "FC2 PPV 1234567 Test Movie | FC2", "2024-01-15", "60", "TestMaker", "Action, Drama", "Actress A", "https://pics.fc2.com/cover.jpg")

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

	result, err := s.Search(context.Background(), "FC2-PPV-1234567")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.ID, "1234567")
}

// TestSearchV5_Disabled tests Search when disabled
func TestSearchV5_Disabled(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://adult.contents.fc2.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	result, err := s.Search(context.Background(), "FC2-PPV-1234567")
	assert.Nil(t, result)
	assert.Error(t, err)
}

// TestSearchV5_InvalidID tests Search with invalid ID format
func TestSearchV5_InvalidID(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://adult.contents.fc2.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.Search(context.Background(), "INVALID-ID")
	assert.Nil(t, result)
	assert.Error(t, err)
}

// TestGetURLV5 tests GetURL
func TestGetURLV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		baseURL:  "https://adult.contents.fc2.com",
		settings: models.ScraperSettings{Enabled: true},
	}

	t.Run("empty ID", func(t *testing.T) {
		_, err := s.GetURL(context.Background(), "")
		assert.Error(t, err)
	})

	t.Run("valid FC2 ID", func(t *testing.T) {
		url, err := s.GetURL(context.Background(), "FC2-PPV-1234567")
		require.NoError(t, err)
		assert.Contains(t, url, "1234567")
	})

	t.Run("invalid format", func(t *testing.T) {
		_, err := s.GetURL(context.Background(), "INVALID")
		assert.Error(t, err)
	})
}

// TestExtractArticleIDV5 tests article ID extraction
func TestExtractArticleIDV5(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"FC2-PPV-1234567", "1234567"},
		{"fc2 ppv 1234567", "1234567"},
		{"1234567", "1234567"},
		{"https://adult.contents.fc2.com/article/1234567/", "1234567"},
		{"INVALID", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := extractArticleID(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestCanonicalFC2IDV5 tests FC2 ID canonicalization
func TestCanonicalFC2IDV5(t *testing.T) {
	assert.Equal(t, "FC2-PPV-1234567", canonicalFC2ID("1234567"))
}

// TestParseDetailPageV5 tests detail page parsing
func TestParseDetailPageV5(t *testing.T) {
	html := buildFC2DetailHTMLV5("1234567", "FC2 PPV 1234567 Test Movie | FC2", "2024-01-15", "60", "TestMaker", "Action, Drama", "Actress A", "https://pics.fc2.com/cover.jpg")

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := parseDetailPage(doc, html, "https://adult.contents.fc2.com/article/1234567/", "1234567")
	require.NotNil(t, result)
	assert.Contains(t, result.ID, "1234567")
}

// TestStripSiteSuffixV5 tests site suffix removal
func TestStripSiteSuffixV5(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Movie Title | FC2", "Movie Title"},
		{"No suffix", "No suffix"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, stripSiteSuffix(tt.input))
		})
	}
}

// TestIsFC2NotFoundPageV5 tests 404 page detection
func TestIsFC2NotFoundPageV5(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected bool
	}{
		{"not found", `<html><body>お探しの商品が見つかりませんでした</body></html>`, true},
		{"normal", `<html><body>Normal page</body></html>`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isFC2NotFoundPage(tt.html))
		})
	}
}

// TestNewScraperV5 tests scraper creation
func TestNewScraperV5(t *testing.T) {
	settings := &models.ScraperSettings{
		Enabled: true,
		BaseURL: "https://adult.contents.fc2.com",
	}

	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	require.NotNil(t, s)
	assert.True(t, s.enabled)
}

// TestScrapeURLV5_NonFC2URL tests ScrapeURL rejecting non-FC2 URLs
func TestScrapeURLV5_NonFC2URL(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://adult.contents.fc2.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.ScrapeURL(context.Background(), "https://example.com/article/1234567")
	assert.Nil(t, result)
	assert.Error(t, err)
}

// TestResolveSearchQueryV5 tests ResolveSearchQuery
func TestResolveSearchQueryV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	result, ok := s.ResolveSearchQuery("FC2-PPV-1234567")
	assert.True(t, ok)
	assert.Equal(t, "FC2-PPV-1234567", result)

	_, ok = s.ResolveSearchQuery("INVALID")
	assert.False(t, ok)
}

// TestFetchPageCtxV5_NetworkError tests fetchPageCtx with connection error
func TestFetchPageCtxV5_NetworkError(t *testing.T) {
	s := &scraper{
		client:      resty.New().SetBaseURL("http://127.0.0.1:1"),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _, err := s.fetchPageCtx(ctx, "http://127.0.0.1:1/nonexistent")
	assert.Error(t, err)
}
