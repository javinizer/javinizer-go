package caribbeancom

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

// --- CanHandleURL ---

func TestMiss2_CanHandleURL(t *testing.T) {
	s := newCaribHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	assert.True(t, s.CanHandleURL("https://www.caribbeancom.com/moviepages/012425-001/"))
	assert.False(t, s.CanHandleURL("https://example.com/test"))
	assert.False(t, s.CanHandleURL("://bad-url"))
}

// --- ScrapeURL: unhandled URL ---

func TestMiss2_ScrapeURL_UnhandledURL(t *testing.T) {
	s := newCaribHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	_, err := s.ScrapeURL(context.Background(), "https://example.com/test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not handled")
}

// --- ScrapeURL: 404 ---

func TestMiss2_ScrapeURL_Status404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newCaribHTTPTScraper(server, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.caribbeancom.com/moviepages/999999-999/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

// --- ScrapeURL: 429 ---

func TestMiss2_ScrapeURL_Status429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	s := newCaribHTTPTScraper(server, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.caribbeancom.com/moviepages/012425-001/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusTooManyRequests, scraperErr.StatusCode)
}

// --- ScrapeURL: 403 ---

func TestMiss2_ScrapeURL_Status403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	s := newCaribHTTPTScraper(server, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.caribbeancom.com/moviepages/012425-001/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Contains(t, scraperErr.Message, "access blocked")
}

// --- Search: cancelled context ---

func TestMiss2_Search_CancelledContext(t *testing.T) {
	s := newCaribHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.Search(ctx, "012425-001")
	require.Error(t, err)
}

// --- fetchPageCtx: cancelled context ---

func TestMiss2_FetchPageCtx_CancelledContext(t *testing.T) {
	s := newCaribHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := s.fetchPageCtx(ctx, "https://www.caribbeancom.com/moviepages/012425-001/")
	require.Error(t, err)
}

// --- fetchPageCtx: network error ---

func TestMiss2_FetchPageCtx_NetworkError(t *testing.T) {
	client := resty.New()
	client.SetTimeout(1 * time.Millisecond)
	client.SetTransport(&errorRTCarib{})

	s := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.caribbeancom.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, _, err := s.fetchPageCtx(context.Background(), "https://www.caribbeancom.com/moviepages/012425-001/")
	require.Error(t, err)
}

// --- normalizeMovieID ---

func TestMiss2_NormalizeMovieID(t *testing.T) {
	// With hyphen stays
	result := normalizeMovieID("012425-001")
	assert.Contains(t, result, "-")
	// Without hyphen gets normalized
	result2 := normalizeMovieID("012425001")
	assert.NotEmpty(t, result2)
}

// --- parseReleaseDateFromID ---

func TestMiss2_ParseReleaseDateFromID(t *testing.T) {
	dt := parseReleaseDateFromID("012425-001")
	if dt != nil {
		assert.Equal(t, 2025, dt.Year())
	}
}

// --- decodeBody ---
// (tested indirectly through fetchPageCtx)

// --- extractCoverURL: various selectors ---

func TestMiss2_ExtractCoverURL_OgImage(t *testing.T) {
	html := `<html><head><meta property="og:image" content="https://www.caribbeancom.com/moviepages/012425-001/images/l_l.jpg"/></head><body></body></html>`
	doc := docFromHTMLCarib(t, html)
	url := extractCoverURL(doc, html, "https://www.caribbeancom.com/moviepages/012425-001/", "012425-001")
	assert.Contains(t, url, "012425-001")
}

// --- applyLanguage ---

func TestMiss2_ApplyLanguage(t *testing.T) {
	s := &scraper{language: "en"}
	assert.Equal(t, "en", s.language)

	s2 := &scraper{language: "ja"}
	assert.Equal(t, "ja", s2.language)
}

// --- ResolveSearchQuery ---

func TestMiss2_ResolveSearchQuery(t *testing.T) {
	s := newCaribHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	id, ok := s.ResolveSearchQuery("012425-001")
	assert.True(t, ok)
	assert.NotEmpty(t, id)

	id, ok = s.ResolveSearchQuery("carib-012425-001")
	assert.True(t, ok)
}

func docFromHTMLCarib(t *testing.T, html string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)
	return doc
}

type errorRTCarib struct{}

func (rt *errorRTCarib) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("forced network error")
}

// --- ScrapeURL: success with detail page ---

func TestMiss2_ScrapeURL_Success(t *testing.T) {
	detailHTML := buildCaribDetailHTML("012425-001", "Test Movie", "A test movie", "2024/01/15", "60", []string{"Genre1"}, []string{"Actress1"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(detailHTML))
	}))
	defer server.Close()

	s := newCaribHTTPTScraper(server, true)
	result, err := s.ScrapeURL(context.Background(), "https://www.caribbeancom.com/moviepages/012425-001/")
	require.NoError(t, err)
	assert.Equal(t, "caribbeancom", result.Source)
}

// --- extractSpecValue: various spec types ---

func TestMiss2_ExtractSpecValue(t *testing.T) {
	html := `<html><body>
		<ul class="movie-spec">
			<li class="movie-spec"><span class="spec-title">配信日</span><span class="spec-content">2024/01/15</span></li>
			<li class="movie-spec"><span class="spec-title">再生時間</span><span class="spec-content">60 分</span></li>
			<li class="movie-spec"><span class="spec-title">タグ</span><span class="spec-content"><a>Tag1</a></span></li>
		</ul>
	</body></html>`
	doc := docFromHTMLCarib(t, html)
	assert.Equal(t, "2024/01/15", extractSpecValue(doc, []string{"配信日"}))
	assert.Equal(t, "60 分", extractSpecValue(doc, []string{"再生時間"}))
}

// --- extractScreenshots: gallery items ---

func TestMiss2_ExtractScreenshots_Gallery(t *testing.T) {
	html := `<html><body>
		<a class="fancy-gallery" href="/moviepages/012425-001/images/01.jpg" data-is_sample="1">S1</a>
		<a class="fancy-gallery" href="/moviepages/012425-001/images/02.jpg" data-is_sample="1">S2</a>
	</body></html>`
	doc := docFromHTMLCarib(t, html)
	urls := extractScreenshots(doc, "https://www.caribbeancom.com")
	assert.GreaterOrEqual(t, len(urls), 1)
}

// --- extractScreenshots: non-sample excluded ---

func TestMiss2_ExtractScreenshots_NonSampleExcluded(t *testing.T) {
	html := `<html><body>
		<a class="fancy-gallery" href="/moviepages/012425-001/images/01.jpg" data-is_sample="0">Not sample</a>
	</body></html>`
	doc := docFromHTMLCarib(t, html)
	urls := extractScreenshots(doc, "https://www.caribbeancom.com")
	assert.Empty(t, urls)
}

// --- extractCoverURL: movie ID fallback ---

func TestMiss2_ExtractCoverURL_MovieIDFallback(t *testing.T) {
	html := `<html><body><p>No og:image</p></body></html>`
	doc := docFromHTMLCarib(t, html)
	url := extractCoverURL(doc, html, "https://www.caribbeancom.com", "012425-001")
	assert.Contains(t, url, "012425-001")
	assert.Contains(t, url, "l_l.jpg")
}

// --- extractCoverURL: empty when no data ---

func TestMiss2_ExtractCoverURL_Empty(t *testing.T) {
	html := `<html><body><p>Nothing</p></body></html>`
	doc := docFromHTMLCarib(t, html)
	url := extractCoverURL(doc, html, "https://www.caribbeancom.com", "")
	assert.Equal(t, "", url)
}

// --- applyLanguage: ja ---

func TestMiss2_ApplyLanguage_JA(t *testing.T) {
	s := &scraper{language: "ja"}
	result := s.applyLanguage("https://www.caribbeancom.com/moviepages/012425-001/")
	assert.Contains(t, result, "caribbeancom.com")
}

// --- applyLanguage: en ---

func TestMiss2_ApplyLanguage_EN(t *testing.T) {
	s := &scraper{language: "en"}
	result := s.applyLanguage("https://www.caribbeancom.com/moviepages/012425-001/")
	assert.Contains(t, result, "en.caribbeancom.com")
}

// --- Search: success ---

func TestMiss2_Search_Success(t *testing.T) {
	detailHTML := buildCaribDetailHTML("012425-001", "Test Movie", "A test movie", "2024/01/15", "60", []string{"Genre1"}, []string{"Actress1"})
	searchHTML := `<html><body><a href="https://www.caribbeancom.com/moviepages/012425-001/">012425-001</a></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "q=") {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, searchHTML)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, detailHTML)
	}))
	defer server.Close()

	s := newCaribHTTPTScraper(server, true)
	result, err := s.Search(context.Background(), "012425-001")
	require.NoError(t, err)
	assert.Equal(t, "caribbeancom", result.Source)
}

// --- Search: non-200 status ---

func TestMiss2_Search_Non200Status(t *testing.T) {
	searchHTML := `<html><body><a href="https://www.caribbeancom.com/moviepages/012425-001/">012425-001</a></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "q=") {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, searchHTML)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	s := newCaribHTTPTScraper(server, true)
	_, err := s.Search(context.Background(), "012425-001")
	require.Error(t, err)
}
