package fc2

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

func TestScrapeURLV4_StatusErrors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.ScrapeURL(context.Background(), ts.URL+"/article/1234567")
	assert.Nil(t, result)
	assert.Error(t, err)
}

func TestSearchV4_Disabled(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://adult.contents.fc2.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	result, err := s.Search(context.Background(), "FC2-1234567")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestParseDetailPageV4(t *testing.T) {
	detailHTML := buildFC2DetailHTML("1234567", "FC2-PPV-1234567 Test Movie", "2024/01/15", "90", "Actress A", "Drama", "https://example.com/cover.jpg")

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(detailHTML))
	require.NoError(t, err)

	result := parseDetailPage(doc, detailHTML, "https://adult.contents.fc2.com/article/1234567", "1234567")
	require.NotNil(t, result)
	// FC2 scraper prefixes with FC2-PPV-
	assert.Contains(t, result.ID, "1234567")
}

func TestCanHandleURLV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	assert.True(t, s.CanHandleURL("https://adult.contents.fc2.com/article/1234567"))
	assert.True(t, s.CanHandleURL("https://fc2.com/article/1234567"))
	assert.False(t, s.CanHandleURL("https://example.com/article/1234567"))
	assert.False(t, s.CanHandleURL(""))
}

func TestExtractIDFromURLV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	id, err := s.ExtractIDFromURL("https://adult.contents.fc2.com/article/1234567")
	assert.NoError(t, err)
	assert.Contains(t, id, "1234567")
}

func TestExtractRatingV4(t *testing.T) {
	html := `<div><span class="user-rating">5.0</span><span>(10)</span></div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	r := extractRating(doc)
	_ = r
}

func TestNormalizeURLV4(t *testing.T) {
	result := normalizeURL("http://adult.contents.fc2.com/article/1234567", "https://adult.contents.fc2.com")
	assert.Contains(t, result, "adult.contents.fc2.com")
}

func buildFC2DetailHTML(id, title, date, runtime, actresses, genres, coverURL string) string {
	return fmt.Sprintf(`<html>
<head><title>%s</title></head>
<body>
<div class="article-header">
	<h2 class="article-title">%s</h2>
</div>
<div class="article-info">
	<section><dl><dt>販売日</dt><dd>%s</dd></dl></section>
	<section><dl><dt>再生時間</dt><dd>%s分</dd></dl></section>
	<section><dl><dt>タグ</dt><dd><a>%s</a></dd></dl></section>
	<section><dl><dt>出演者</dt><dd><a>%s</a></dd></dl></section>
</div>
<div class="article-main"><img src="%s" /></div>
</body>
</html>`, title, title, date, runtime, genres, actresses, coverURL)
}
