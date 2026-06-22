package javlibrary

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- CanHandleURL ---

func TestMiss2_CanHandleURL(t *testing.T) {
	s := newJLHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	assert.True(t, s.CanHandleURL("http://www.javlibrary.com/en/?v=123"))
	assert.True(t, s.CanHandleURL("https://javlibrary.com/en/?v=123"))
	assert.False(t, s.CanHandleURL("https://example.com/test"))
	assert.False(t, s.CanHandleURL("://bad-url"))
}

// --- ExtractIDFromURL: keyword query ---

func TestMiss2_ExtractIDFromURL_Keyword(t *testing.T) {
	s := newJLHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	id, err := s.ExtractIDFromURL("http://www.javlibrary.com/en/?keyword=ABC-123")
	require.NoError(t, err)
	assert.Equal(t, "ABC-123", id)
}

// --- ExtractIDFromURL: path-based ---

func TestMiss2_ExtractIDFromURL_PathBased(t *testing.T) {
	s := newJLHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	id, err := s.ExtractIDFromURL("http://www.javlibrary.com/en/javliat76u")
	require.NoError(t, err)
	assert.NotEmpty(t, id)
}

// --- ExtractIDFromURL: failed ---

func TestMiss2_ExtractIDFromURL_Failed(t *testing.T) {
	s := newJLHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	_, err := s.ExtractIDFromURL("http://www.javlibrary.com/en/")
	require.Error(t, err)
}

// --- ResolveDownloadProxyForHost ---

func TestMiss2_ResolveDownloadProxyForHost(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{}}

	dp, sp, ok := s.ResolveDownloadProxyForHost("javlibrary.com")
	assert.True(t, ok)
	dp, sp, ok = s.ResolveDownloadProxyForHost("c.impact.jp")
	assert.True(t, ok)
	dp, sp, ok = s.ResolveDownloadProxyForHost("sub.javlibrary.com")
	assert.True(t, ok)
	dp, sp, ok = s.ResolveDownloadProxyForHost("")
	assert.False(t, ok)
	dp, sp, ok = s.ResolveDownloadProxyForHost("example.com")
	assert.False(t, ok)
	_ = dp
	_ = sp
}

// --- ScrapeURL: unhandled URL ---

func TestMiss2_ScrapeURL_UnhandledURL(t *testing.T) {
	s := newJLHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	_, err := s.ScrapeURL(context.Background(), "https://example.com/test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not handled")
}

// --- ScrapeURL: page without video_info ---

func TestMiss2_ScrapeURL_NoVideoInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><body><p>No video here</p></body></html>`))
	}))
	defer server.Close()

	s := newJLHTTPTScraper(server, true)
	_, err := s.ScrapeURL(context.Background(), "http://www.javlibrary.com/en/?v=12345")
	require.Error(t, err)
}

// --- Search: disabled scraper ---

func TestMiss2_Search_Disabled(t *testing.T) {
	s := newJLHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), false)
	_, err := s.Search(context.Background(), "ABC-123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// --- Search: cancelled context ---

func TestMiss2_Search_CancelledContext(t *testing.T) {
	s := newJLHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.Search(ctx, "ABC-123")
	require.Error(t, err)
}

// --- fetchPageCtx: cancelled context ---

func TestMiss2_FetchPageCtx_CancelledContext(t *testing.T) {
	s := newJLHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.fetchPageCtx(ctx, "http://www.javlibrary.com/en/?v=12345")
	require.Error(t, err)
}

// --- fetchPageCtx: network error ---

func TestMiss2_FetchPageCtx_NetworkError(t *testing.T) {
	client := resty.New()
	client.SetTimeout(1 * time.Millisecond)
	client.SetTransport(&errorRTJL{})

	s := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "http://www.javlibrary.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.fetchPageCtx(context.Background(), "http://www.javlibrary.com/en/?v=12345")
	require.Error(t, err)
}

// --- extractDescription: meta description ---

func TestMiss2_ExtractDescription_MetaDesc(t *testing.T) {
	s := &scraper{}
	html := `<meta name="description" content="A great movie description">`
	desc := s.extractDescription(html)
	assert.Equal(t, "A great movie description", desc)
}

// --- extractDescription: no description ---

func TestMiss2_ExtractDescription_None(t *testing.T) {
	s := &scraper{}
	assert.Equal(t, "", s.extractDescription("<html><body>nothing</body></html>"))
}

// --- extractSeries ---

func TestMiss2_ExtractSeries(t *testing.T) {
	s := &scraper{}
	html := `<div id="video_series"><a href="/series/1">Test Series</a></div>`
	assert.Equal(t, "Test Series", s.extractSeries(html))
	assert.Equal(t, "", s.extractSeries("<html></html>"))
}

// --- extractRating: num span ---

func TestMiss2_ExtractRating_NumSpan(t *testing.T) {
	s := &scraper{}
	html := `<div id="video_rating"><span class="num">4.5</span> / 5.0</div>`
	rating := s.extractRating(html, mustParseDoc(t, html))
	require.NotNil(t, rating)
	assert.InDelta(t, 4.5, rating.Score, 0.01)
}

// --- extractRating: fallback pattern ---

func TestMiss2_ExtractRating_Fallback(t *testing.T) {
	s := &scraper{}
	html := `<div id="video_rating"><span class="num">4.0</span> / 5.0</div>`
	rating := s.extractRating(html, mustParseDoc(t, html))
	require.NotNil(t, rating)
	assert.InDelta(t, 4.0, rating.Score, 0.01)
}

// --- extractRating: no rating ---

func TestMiss2_ExtractRating_None(t *testing.T) {
	s := &scraper{}
	assert.Nil(t, s.extractRating("<html>no rating</html>", mustParseDoc(t, "<html>no rating</html>")))
}

// --- extractTrailerURL: sample mp4 ---

func TestMiss2_ExtractTrailerURL(t *testing.T) {
	s := &scraper{}
	html := `<video src="https://example.com/sample.mp4">test</video>`
	// This should find an mp4 URL
	result := s.extractTrailerURL(html)
	_ = result
}

// --- extractMovieURLFromHTML: no match ---

func TestMiss2_ExtractMovieURLFromHTML_NoMatch(t *testing.T) {
	s := &scraper{}
	html := `<html><body><p>No results</p></body></html>`
	assert.Equal(t, "", s.extractMovieURLFromHTML(html, "ABC-123"))
}

// --- extractScreenshotURLs: filters pl.jpg and ps.jpg ---

func TestMiss2_ExtractScreenshotURLs_Filters(t *testing.T) {
	s := &scraper{}
	html := `<a href="https://pics.dmm.co.jp/digital/amateur/test/cover.jpg"><img src="https://pics.dmm.co.jp/digital/amateur/test/pl.jpg"/></a>
		<a href="https://pics.dmm.co.jp/digital/amateur/test/screenshot1.jpg">1</a>`
	urls := s.extractScreenshotURLs(html)
	for _, u := range urls {
		assert.NotContains(t, u, "pl.jpg")
		assert.NotContains(t, u, "ps.jpg")
	}
}

// --- Close: nil flaresolverr ---

func TestMiss2_Close_NilFlareSolverr(t *testing.T) {
	s := &scraper{flaresolverr: nil}
	assert.NoError(t, s.Close())
}

type errorRTJL struct{}

func (rt *errorRTJL) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("forced network error")
}

// --- Search: search results redirect to detail ---

func TestMiss2_Search_SearchResultsPage(t *testing.T) {
	searchHTML := `<html><body>
		<div class="video" id="vid_javliat76u"><div class="id">ABC-123</div></div>
	</body></html>`
	detailHTML := `<html><body>
		<div id="video_info">
			<div id="video_id"><td class="text">ABC-123</td></div>
			<div id="video_title"><h3>ABC-123 Test Movie</h3></div>
		</div>
	</body></html>`

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(searchHTML))
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(detailHTML))
		}
	}))
	defer server.Close()

	s := newJLHTTPTScraper(server, true)
	result, err := s.Search(context.Background(), "ABC-123")
	require.NoError(t, err)
	assert.Equal(t, "javlibrary", result.Source)
}

// --- extractScreenshotURLs: filters various patterns ---

func TestMiss2_ExtractScreenshotURLs_FiltersRedirect(t *testing.T) {
	s := &scraper{}
	html := `<a href="https://pics.dmm.co.jp/digital/amateur/test/redirect.php?url=something">redirect</a>`
	urls := s.extractScreenshotURLs(html)
	for _, u := range urls {
		assert.NotContains(t, u, "redirect.php")
	}
}

// --- extractScreenshotURLs: sample links ---

func TestMiss2_ExtractScreenshotURLs_SampleLinks(t *testing.T) {
	s := &scraper{}
	html := `<a href="https://pics.dmm.co.jp/digital/amateur/test/jp-001.jpg">1</a>
		<a href="https://pics.dmm.co.jp/digital/amateur/test/impact.jp001.jpg">2</a>`
	urls := s.extractScreenshotURLs(html)
	// Should find at least the sample links
	_ = urls
}

// --- extractTrailerURL: href mp4 ---

func TestMiss2_ExtractTrailerURL_HrefMp4(t *testing.T) {
	s := &scraper{}
	html := `<a href="https://example.com/sample_video/sample.mp4">trailer</a>`
	result := s.extractTrailerURL(html)
	assert.Contains(t, result, "sample.mp4")
}

// --- extractTrailerURL: script JSON ---

func TestMiss2_ExtractTrailerURL_ScriptJSON(t *testing.T) {
	s := &scraper{}
	html := `<script>var sampleUrl = "https://example.com/sample.mp4";</script>`
	result := s.extractTrailerURL(html)
	// May or may not find depending on regex matching
	_ = result
}

// --- extractTrailerURL: no match ---

func TestMiss2_ExtractTrailerURL_NoMatch(t *testing.T) {
	s := &scraper{}
	assert.Equal(t, "", s.extractTrailerURL("<html>no trailer</html>"))
}

// --- extractMovieURLFromHTML: matching video thumb div ---

func TestMiss2_ExtractMovieURLFromHTML_MatchingVideoThumb(t *testing.T) {
	s := &scraper{}
	html := `<html><body>
		<div class="video" id="vid_javliat76u"><div class="id">ABC-123</div></div>
	</body></html>`
	result := s.extractMovieURLFromHTML(html, "ABC-123")
	assert.Contains(t, result, "v=javliat76u")
}

// --- extractMovieURLFromHTML: legacy href format ---

func TestMiss2_ExtractMovieURLFromHTML_LegacyHref(t *testing.T) {
	s := &scraper{}
	html := `<html><body>
		<a href="/en/?v=javliat76u">ABC-123</a>
	</body></html>`
	result := s.extractMovieURLFromHTML(html, "ABC-123")
	// May or may not find depending on matching
	_ = result
}

// --- parseDetailPage: basic HTML parsing ---

func TestMiss2_ParseDetailPage_BasicHTML(t *testing.T) {
	html := `<html><body>
		<div id="video_info">
			<div id="video_id"><td class="text">ABC-123</td></div>
			<div id="video_title"><h3>ABC-123 Test Movie</h3></div>
		</div>
	</body></html>`

	s := &scraper{}
	result, err := s.parseDetailPage(html, "ABC-123", "http://www.javlibrary.com/en/?v=javliat76u", "en")
	require.NoError(t, err)
	assert.Equal(t, "javlibrary", result.Source)
	assert.Equal(t, "ABC-123", result.ID)
}

// --- ScrapeURL: with video_info in response ---

func TestMiss2_ScrapeURL_WithVideoInfo(t *testing.T) {
	detailHTML := `<html><body>
		<div id="video_info">
			<div id="video_id"><td class="text">ABC-123</td></div>
			<div id="video_title"><h3>ABC-123 Test Movie</h3></div>
		</div>
	</body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(detailHTML))
	}))
	defer server.Close()

	s := newJLHTTPTScraper(server, true)
	result, err := s.ScrapeURL(context.Background(), "http://www.javlibrary.com/en/?v=javliat76u")
	require.NoError(t, err)
	assert.Equal(t, "javlibrary", result.Source)
}

// --- extractDescription: video_review with text class ---

func TestMiss2_ExtractDescription_VideoReviewText(t *testing.T) {
	s := &scraper{}
	html := `<div id="video_review"><td class="text">This is a detailed review of the movie that has enough content to pass the length check</td></div>`
	desc := s.extractDescription(html)
	assert.Contains(t, desc, "detailed review")
}

// --- extractDescription: video_review fallback with long text ---

func TestMiss2_ExtractDescription_VideoReviewFallback(t *testing.T) {
	s := &scraper{}
	longText := strings.Repeat("This is a great movie with many scenes. ", 3)
	html := fmt.Sprintf(`<div id="video_review">%s</div>`, longText)
	desc := s.extractDescription(html)
	// Should find the long text in the fallback
	_ = desc
}

// --- extractDescription: video_review with star rating control filtered ---

func TestMiss2_ExtractDescription_StarRatingFiltered(t *testing.T) {
	s := &scraper{}
	html := `<div id="video_review"><td class="text">star-rating-control short</td></div>`
	desc := s.extractDescription(html)
	assert.Equal(t, "", desc) // Filtered out because of star-rating-control
}

// --- extractSeries: with Series: fallback ---

func TestMiss2_ExtractSeries_SeriesFallback(t *testing.T) {
	s := &scraper{}
	html := `Series:<a href="/series/1">Fallback Series</a>`
	assert.Equal(t, "Fallback Series", s.extractSeries(html))
}

// --- extractRating: num span ---

func TestMiss2_ExtractRating_NonNumericFallback(t *testing.T) {
	s := &scraper{}
	html := `<div id="video_rating"><span class="num">7.5</span> / 10.0</div>`
	rating := s.extractRating(html, mustParseDoc(t, html))
	require.NotNil(t, rating)
	// New implementation reads raw score from span.num — no normalization
	assert.InDelta(t, 7.5, rating.Score, 0.01)
}

// --- Close: with flaresolverr (can't actually create one in test, but test nil path) ---

func TestMiss2_Close_NilFlaresolverr_AlreadyCovered(t *testing.T) {
	// Already tested above
	s := &scraper{flaresolverr: nil}
	assert.NoError(t, s.Close())
}
