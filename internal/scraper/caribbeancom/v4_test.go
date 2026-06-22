package caribbeancom

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
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.ScrapeURL(context.Background(), ts.URL+"/eng/movie/012345_678")
	assert.Nil(t, result)
	assert.Error(t, err)
}

func TestSearchV4_Disabled(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.caribbeancom.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	result, err := s.Search(context.Background(), "012345-678")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestParseDetailPageV4(t *testing.T) {
	detailHTML := buildCaribbeanDetailHTML("012345-678", "Test Movie", "2024/01/15", "90", "Actress A, Actress B", "Drama, Romance", "https://example.com/cover.jpg", "https://example.com/thumb.jpg")

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(detailHTML))
	require.NoError(t, err)

	result := parseDetailPage(doc, detailHTML, "https://www.caribbeancom.com/eng/movie/012345_678/", "012345-678", "ja")
	require.NotNil(t, result)
	assert.Equal(t, "012345-678", result.ID)
}

func TestCanHandleURLV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	assert.True(t, s.CanHandleURL("https://www.caribbeancom.com/eng/movie/012345_678/"))
	assert.True(t, s.CanHandleURL("https://caribbeancom.com/movie/012345_678/"))
	assert.False(t, s.CanHandleURL("https://example.com/eng/movie/012345_678/"))
	assert.False(t, s.CanHandleURL(""))
}

func TestExtractIDFromURLV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	// ExtractIDFromURL uses movieIDFromPageRe which expects /moviepages/ path
	id, err := s.ExtractIDFromURL("https://www.caribbeancom.com/moviepages/012345_678/")
	assert.NoError(t, err)
	assert.Equal(t, "012345-678", id)
}

func TestResolveSearchQueryV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	url, ok := s.ResolveSearchQuery("https://www.caribbeancom.com/moviepages/012345_678/")
	assert.True(t, ok)
	assert.NotEmpty(t, url)

	_, ok = s.ResolveSearchQuery("not-a-url")
	assert.False(t, ok)
}

func TestNormalizeMovieIDV4(t *testing.T) {
	// normalizeMovieID converts _ to -
	assert.Equal(t, "012345-678", normalizeMovieID("012345_678"))
	assert.Equal(t, "012345-678", normalizeMovieID("012345-678"))
}

func buildCaribbeanDetailHTML(id, title, date, runtime, actresses, genres, coverURL, thumbURL string) string {
	return fmt.Sprintf(`<html>
<head><title>%s</title></head>
<body>
<div class="movie-info">
	<h1 itemprop="name">%s</h1>
	<span itemprop="datePublished">%s</span>
	<span>%s min</span>
	<div class="movie-spec">
		<dt>出演</dt><dd>%s</dd>
		<dt>タグ</dt><dd>%s</dd>
	</div>
</div>
<div class="movie-thumnail"><img src="%s" /></div>
<div class="movie-cover"><img src="%s" /></div>
</body>
</html>`, id, title, date, runtime, actresses, genres, thumbURL, coverURL)
}
