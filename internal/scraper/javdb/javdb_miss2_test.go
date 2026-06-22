package javdb

import (
	"context"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- ScrapeURL: extract ID failure (URL without /v/ path) ---

func TestScrapeURL_Miss2_ExtractIDFailure(t *testing.T) {
	s := &scraper{
		enabled:     true,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}

	// URL is on javdb.com but doesn't have /v/ path, so ExtractIDFromURL fails
	// This tests the "videoID = ''" path
	_, err := s.ScrapeURL(context.Background(), "https://javdb.com/search?q=test")
	// Should still try to scrape, might fail due to network, but shouldn't panic
	// The key test is that it doesn't crash when ExtractIDFromURL fails
	_ = err
}

// --- ScrapeURL: sparse detail page triggers direct retry ---

func TestScrapeURL_Miss2_SparseDetailRetrySuccess(t *testing.T) {
	sparseHTML := `<html><body><p>Not much here</p></body></html>`
	fullHTML := `
<html>
	<head><title>IPX-888 Sparse Retry - JavDB</title></head>
	<body>
		<h2 class="title is-4"><strong>IPX-888</strong> Sparse Retry Movie</h2>
		<div class="column-video-cover"><img class="video-cover" src="https://img.example.com/cover.jpg" /></div>
		<div class="movie-panel-info">
			<div class="panel-block"><strong>片商:</strong><span class="value"><a>Retry Maker</a></span></div>
		</div>
	</body>
</html>`

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(sparseHTML))
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fullHTML))
		}
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&javdbTestTransport{server: server})
	client.SetTimeout(5 * time.Second)

	s := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.ScrapeURL(context.Background(), "https://javdb.com/v/ipx888")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-888", result.ID)
	assert.Equal(t, "Retry Maker", result.Maker)
	assert.True(t, callCount >= 2, "expected retry after sparse response")
}

// --- ScrapeURL: sparse detail page, direct retry also fails ---

func TestScrapeURL_Miss2_SparseRetryAlsoFails(t *testing.T) {
	sparseHTML := `<html><body><p>Still sparse</p></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sparseHTML))
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&javdbTestTransport{server: server})
	client.SetTimeout(5 * time.Second)

	s := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.ScrapeURL(context.Background(), "https://javdb.com/v/sparse2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-detail content")
}

// --- Search: retry path when detail page is sparse ---

func TestSearch_Miss2_SparseDetailRetry(t *testing.T) {
	searchHTML := `
<html><body>
	<div class="movie-list">
		<div class="item">
			<a href="/v/ret123">
				<div class="video-title"><strong>RET-001</strong> Retry Search</div>
				<div class="uid">RET-001</div>
			</a>
		</div>
	</div>
</body></html>`

	sparseHTML := `<html><body><p>Sparse</p></body></html>`
	fullHTML := `
<html><body>
	<h2 class="title is-4"><strong>RET-001</strong> Retry Search</h2>
	<div class="column-video-cover"><img class="video-cover" src="https://img.example.com/cover.jpg" /></div>
	<div class="movie-panel-info">
		<div class="panel-block"><strong>片商:</strong><span class="value"><a>Retry Search Maker</a></span></div>
	</div>
</body></html>`

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		path := r.URL.Path
		if strings.Contains(path, "/search") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(searchHTML))
		} else if callCount <= 2 {
			// First detail fetch returns sparse
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(sparseHTML))
		} else {
			// Retry detail fetch returns full
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fullHTML))
		}
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&javdbTestTransport{server: server})
	client.SetTimeout(5 * time.Second)

	s := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://javdb.test",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.Search(context.Background(), "RET-001")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Retry Search Maker", result.Maker)
}

// --- Search: retry also returns sparse ---

func TestSearch_Miss2_RetryAlsoSparse(t *testing.T) {
	searchHTML := `
<html><body>
	<div class="movie-list">
		<div class="item">
			<a href="/v/spa123">
				<div class="video-title"><strong>SPA-001</strong> Sparse</div>
				<div class="uid">SPA-001</div>
			</a>
		</div>
	</div>
</body></html>`

	sparseHTML := `<html><body><p>Sparse</p></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.Contains(path, "/search") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(searchHTML))
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(sparseHTML))
		}
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&javdbTestTransport{server: server})
	client.SetTimeout(5 * time.Second)

	s := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://javdb.test",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.Search(context.Background(), "SPA-001")
	require.Error(t, err)
}

// --- fetchPageDirectCtx: context cancelled ---

func TestFetchPageDirectCtx_Miss2_CancelledContext(t *testing.T) {
	s := &scraper{
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.fetchPageDirectCtx(ctx, "https://javdb.com/test")
	require.Error(t, err)
}

// --- fetchPageDirectCtx: non-200 returns status error ---

func TestFetchPageDirectCtx_Miss2_Non200Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&javdbTestTransport{server: server})
	client.SetTimeout(5 * time.Second)

	s := &scraper{
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
	}

	_, err := s.fetchPageDirectCtx(context.Background(), "https://javdb.com/test")
	require.Error(t, err)
}

// --- hasDetailMetadata: nil result ---

func TestHasDetailMetadata_Miss2_NilResult(t *testing.T) {
	assert.False(t, hasDetailMetadata(nil, "fallback"))
}

// --- hasDetailMetadata: title matches fallback ID ---

func TestHasDetailMetadata_Miss2_TitleMatchesFallback(t *testing.T) {
	result := &models.ScraperResult{Title: "SAME-ID"}
	assert.False(t, hasDetailMetadata(result, "SAME-ID"))
}

// --- hasDetailMetadata: title differs from fallback ---

func TestHasDetailMetadata_Miss2_TitleDiffersFromFallback(t *testing.T) {
	result := &models.ScraperResult{Title: "Real Title"}
	assert.True(t, hasDetailMetadata(result, "FALLBACK"))
}

// --- hasDetailMetadata: has cover URL ---

func TestHasDetailMetadata_Miss2_CoverURL(t *testing.T) {
	result := &models.ScraperResult{CoverURL: "https://example.com/cover.jpg"}
	assert.True(t, hasDetailMetadata(result, ""))
}

// --- hasDetailMetadata: has runtime ---

func TestHasDetailMetadata_Miss2_Runtime(t *testing.T) {
	result := &models.ScraperResult{Runtime: 120}
	assert.True(t, hasDetailMetadata(result, ""))
}

// --- hasDetailMetadata: has release date ---

func TestHasDetailMetadata_Miss2_ReleaseDate(t *testing.T) {
	now := time.Now()
	result := &models.ScraperResult{ReleaseDate: &now}
	assert.True(t, hasDetailMetadata(result, ""))
}

// --- hasDetailMetadata: has actresses ---

func TestHasDetailMetadata_Miss2_Actresses(t *testing.T) {
	result := &models.ScraperResult{Actresses: []models.ActressInfo{{DMMID: 1}}}
	assert.True(t, hasDetailMetadata(result, ""))
}

// --- hasDetailMetadata: has genres ---

func TestHasDetailMetadata_Miss2_Genres(t *testing.T) {
	result := &models.ScraperResult{Genres: []string{"Drama"}}
	assert.True(t, hasDetailMetadata(result, ""))
}

// --- hasDetailMetadata: has screenshots ---

func TestHasDetailMetadata_Miss2_Screenshots(t *testing.T) {
	result := &models.ScraperResult{ScreenshotURL: []string{"https://example.com/shot.jpg"}}
	assert.True(t, hasDetailMetadata(result, ""))
}

// --- isJavDBVideoCode edge cases ---

func TestIsJavDBVideoCode_Miss2_EdgeCases(t *testing.T) {
	assert.False(t, isJavDBVideoCode(""), "empty string")
	assert.False(t, isJavDBVideoCode("ab"), "too short")
	assert.False(t, isJavDBVideoCode("a-very-long-code-that-exceeds"), "too long")
	assert.False(t, isJavDBVideoCode("IPX-535"), "hyphen not allowed")
	assert.True(t, isJavDBVideoCode("AbJEe"), "valid code")
	assert.True(t, isJavDBVideoCode("abc123"), "valid alphanumeric")
}

// --- ExtractIDFromURL: parse error ---

func TestExtractIDFromURL_Miss2_ParseError(t *testing.T) {
	s := &scraper{baseURL: "https://javdb.com"}
	_, err := s.ExtractIDFromURL("://not-a-valid-url")
	require.Error(t, err)
}

// --- ExtractIDFromURL: no /v/ path ---

func TestExtractIDFromURL_Miss2_NoVPath(t *testing.T) {
	s := &scraper{baseURL: "https://javdb.com"}
	_, err := s.ExtractIDFromURL("https://javdb.com/search?q=test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "extract ID")
}

// --- normalizeIDForCompare edge cases ---

func TestNormalizeIDForCompare_Miss2(t *testing.T) {
	assert.Equal(t, "IPX535", normalizeIDForCompare("IPX-535"))
	assert.Equal(t, "IPX535", normalizeIDForCompare("ipx-535"))
	assert.Equal(t, "", normalizeIDForCompare(""))
	assert.Equal(t, "ABC123", normalizeIDForCompare("  ABC-123  "))
}

// --- trimVariantSuffix edge cases ---

func TestTrimVariantSuffix_Miss2(t *testing.T) {
	assert.Equal(t, "IPX535", trimVariantSuffix("IPX535A"))
	assert.Equal(t, "IP", trimVariantSuffix("IP"))         // too short (len < 2)
	assert.Equal(t, "IPX535", trimVariantSuffix("IPX535")) // last char is digit, not letter after digit
	assert.Equal(t, "AB", trimVariantSuffix("AB"))         // A is letter, B is letter, but B is not after a digit
}

// --- trimNumericPadding edge cases ---

func TestTrimNumericPadding_Miss2(t *testing.T) {
	assert.Equal(t, "IPX5", trimNumericPadding("IPX005"))
	assert.Equal(t, "IPX0", trimNumericPadding("IPX000"))
	assert.Equal(t, "IPX", trimNumericPadding("IPX")) // no digits
	assert.Equal(t, "IPX5A", trimNumericPadding("IPX005A"))
}

// --- classifyCastLabel edge cases ---

func TestClassifyCastLabel_Miss2(t *testing.T) {
	assert.Equal(t, castLabelMale, classifyCastLabel("男優"))
	assert.Equal(t, castLabelMale, classifyCastLabel("male actor"))
	assert.Equal(t, castLabelFemale, classifyCastLabel("女優"))
	assert.Equal(t, castLabelFemale, classifyCastLabel("actress"))
	assert.Equal(t, castLabelGeneric, classifyCastLabel("演員"))
	assert.Equal(t, castLabelGeneric, classifyCastLabel("actor"))
	assert.Equal(t, castLabelGeneric, classifyCastLabel("出演者"))
	assert.Equal(t, castLabelGeneric, classifyCastLabel("cast"))
	assert.Equal(t, castLabelUnknown, classifyCastLabel("other"))
}

// --- isNotAvailableValue edge cases ---

func TestIsNotAvailableValue_Miss2(t *testing.T) {
	assert.False(t, isNotAvailableValue(""))
	assert.True(t, isNotAvailableValue("N/A"))
	assert.True(t, isNotAvailableValue("n.a."))
	assert.True(t, isNotAvailableValue("none"))
	assert.True(t, isNotAvailableValue("null"))
	assert.True(t, isNotAvailableValue("nil"))
	assert.True(t, isNotAvailableValue("無し"))
	assert.True(t, isNotAvailableValue("なし"))
	assert.True(t, isNotAvailableValue("-"))
	assert.True(t, isNotAvailableValue("--"))
	assert.True(t, isNotAvailableValue("notavailable"))
	assert.True(t, isNotAvailableValue("notapplicable"))
	assert.False(t, isNotAvailableValue("Drama"))
}

// --- parseDetailPage: generic cast fallback when no female row ---

func TestParseDetailPage_Miss2_GenericCastFallback(t *testing.T) {
	html := `
<html><body>
	<h2 class="title is-4"><strong>GEN-001</strong> Generic Cast Test</h2>
	<div class="movie-panel-info">
		<div class="panel-block"><strong>演員:</strong><span class="value">
			<a href="/a/1">Generic Actress</a>
		</span></div>
	</div>
</body></html>`
	doc := docFromHTMLMiss(t, html)

	s := &scraper{baseURL: "https://javdb.com"}
	result, err := s.parseDetailPage(doc, "https://javdb.com/v/test", "GEN-001")
	require.NoError(t, err)
	assert.Len(t, result.Actresses, 1)
}

// --- parseDetailPage: generic cast NOT used as fallback when female row exists ---

func TestParseDetailPage_Miss2_GenericCastSkippedWhenFemaleExists(t *testing.T) {
	html := `
<html><body>
	<h2 class="title is-4"><strong>MIX-001</strong> Mixed Cast Test</h2>
	<div class="movie-panel-info">
		<div class="panel-block"><strong>女優:</strong><span class="value">
			<a href="/a/1">Female Actress</a>
		</span></div>
		<div class="panel-block"><strong>演員:</strong><span class="value">
			<a href="/a/2">Generic Actor</a>
		</span></div>
	</div>
</body></html>`
	doc := docFromHTMLMiss(t, html)

	s := &scraper{baseURL: "https://javdb.com"}
	result, err := s.parseDetailPage(doc, "https://javdb.com/v/test", "MIX-001")
	require.NoError(t, err)
	// Female row should be used, generic should be skipped
	assert.Len(t, result.Actresses, 1)
	assert.Equal(t, "Female Actress", result.Actresses[0].JapaneseName)
}

// --- parseDetailPage: male-only cast row excluded ---

func TestParseDetailPage_Miss2_MaleOnlyRowExcluded(t *testing.T) {
	html := `
<html><body>
	<h2 class="title is-4"><strong>MAL-001</strong> Male Only Test</h2>
	<div class="movie-panel-info">
		<div class="panel-block"><strong>男優:</strong><span class="value">
			<a href="/a/1">Male Actor</a>
		</span></div>
	</div>
</body></html>`
	doc := docFromHTMLMiss(t, html)

	s := &scraper{baseURL: "https://javdb.com"}
	result, err := s.parseDetailPage(doc, "https://javdb.com/v/test", "MAL-001")
	require.NoError(t, err)
	assert.Len(t, result.Actresses, 0)
}

// --- extractTrailerURL: no video element ---

func TestExtractTrailerURL_Miss2_NoVideo(t *testing.T) {
	html := `<html><body><p>No trailer here</p></body></html>`
	doc := docFromHTMLMiss(t, html)

	url := extractTrailerURL(doc, "https://javdb.com")
	assert.Empty(t, url)
}

// --- extractScreenshotURLs: no preview images ---

func TestExtractScreenshotURLs_Miss2_NoImages(t *testing.T) {
	html := `<html><body><p>No images</p></body></html>`
	doc := docFromHTMLMiss(t, html)

	urls := extractScreenshotURLs(doc, "https://javdb.com")
	assert.Empty(t, urls)
}

// --- parseRating: score between 5 and 10 ---

func TestParseRating_Miss2_ScoreBetween5And10(t *testing.T) {
	r := parseRating("7.5分 (200人)")
	require.NotNil(t, r)
	assert.InDelta(t, 7.5, r.Score, 0.001)
	assert.Equal(t, 200, r.Votes)
}

// --- parseRating: single number without votes ---

func TestParseRating_Miss2_ScoreNoVotes(t *testing.T) {
	r := parseRating("4.2")
	require.NotNil(t, r)
	assert.InDelta(t, 8.4, r.Score, 0.001) // 4.2 * 2 since <= 5
}

// --- extractFirstURL: src attribute fallback ---

func TestExtractFirstURL_Miss2_SrcAttr(t *testing.T) {
	html := `<img class="video-cover" src="https://img.example.com/cover.jpg" />`
	doc := docFromHTMLMiss(t, html)

	url := extractFirstURL(doc, []string{"img.video-cover"}, "https://javdb.com")
	assert.Equal(t, "https://img.example.com/cover.jpg", url)
}

// --- extractFirstURL: data-original preferred over src ---

func TestExtractFirstURL_Miss2_DataOriginalPreferred(t *testing.T) {
	html := `<img class="video-cover" data-original="https://img.example.com/lazy.jpg" src="https://img.example.com/placeholder.jpg" />`
	doc := docFromHTMLMiss(t, html)

	url := extractFirstURL(doc, []string{"img.video-cover"}, "https://javdb.com")
	assert.Equal(t, "https://img.example.com/lazy.jpg", url)
}

// --- labelContains: case insensitive ---

func TestLabelContains_Miss2_CaseInsensitive(t *testing.T) {
	// labelContains lowercases the keys but NOT the label - caller should normalize
	assert.True(t, labelContains("release", "release"))
	assert.True(t, labelContains("release", "RELEASE")) // key is lowercased by the function
	assert.False(t, labelContains("other", "release"))
}

// --- hasWordToken edge cases ---

func TestHasWordToken_Miss2(t *testing.T) {
	assert.True(t, hasWordToken("male actor", "male"))
	assert.True(t, hasWordToken("female actress", "female"))
	assert.False(t, hasWordToken("female", "male"))
	assert.False(t, hasWordToken("", "male"))
}

// --- GetURL: trims whitespace ---

func TestGetURL_Miss2_TrimsWhitespace(t *testing.T) {
	s := &scraper{baseURL: "https://javdb.com"}
	url, err := s.GetURL(context.Background(), "  IPX-535  ")
	require.NoError(t, err)
	assert.Contains(t, url, "IPX-535")
}

// --- idMatchRank edge cases ---

func TestIdMatchRank_Miss2_EdgeCases(t *testing.T) {
	assert.Equal(t, idMatchNone, idMatchRank("", "IPX535"))
	assert.Equal(t, idMatchNone, idMatchRank("IPX535", ""))
	assert.Equal(t, idMatchExact, idMatchRank("IPX-535", "ipx-535"))
	assert.Equal(t, idMatchNormalized, idMatchRank("IPX00535", "IPX535"))
	assert.Equal(t, idMatchVariant, idMatchRank("IPX535A", "IPX535"))
}
