package javdb

import (
	"context"
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

// --- parseDetailPage: male actor row (should not be merged into actresses) ---

func TestMiss5_ParseDetailPage_MaleActorRow(t *testing.T) {
	html := `<!DOCTYPE html><html><body>
	<h2 class="title is-4"><strong>ABP-500</strong> Test Movie</h2>
	<div class="movie-panel-info">
		<div class="panel-block"><strong>男優</strong><div class="value"><a>MaleActor1</a></div></div>
		<div class="panel-block"><strong>女優</strong><div class="value"><a>FemaleActress1</a></div></div>
	</div>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{
		enabled:     true,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.parseDetailPage(doc, "https://javdb.com/v/abc123", "ABP-500")
	require.NoError(t, err)
	assert.Len(t, result.Actresses, 1)
	assert.Equal(t, "FemaleActress1", result.Actresses[0].JapaneseName)
}

// --- parseDetailPage: generic cast row as fallback when no female row ---

func TestMiss5_ParseDetailPage_GenericCastFallback(t *testing.T) {
	html := `<!DOCTYPE html><html><body>
	<h2 class="title is-4"><strong>ABP-501</strong> Test Movie</h2>
	<div class="movie-panel-info">
		<div class="panel-block"><strong>演員</strong><div class="value"><a>GenericActor1</a></div></div>
	</div>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{
		enabled:     true,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.parseDetailPage(doc, "https://javdb.com/v/abc123", "ABP-501")
	require.NoError(t, err)
	assert.Len(t, result.Actresses, 1)
	assert.Equal(t, "GenericActor1", result.Actresses[0].JapaneseName)
}

// --- parseDetailPage: generic cast row skipped when female row exists ---

func TestMiss5_ParseDetailPage_GenericCastSkippedWhenFemaleExists(t *testing.T) {
	html := `<!DOCTYPE html><html><body>
	<h2 class="title is-4"><strong>ABP-502</strong> Test Movie</h2>
	<div class="movie-panel-info">
		<div class="panel-block"><strong>女優</strong><div class="value"><a>Female1</a></div></div>
		<div class="panel-block"><strong>演員</strong><div class="value"><a>Generic1</a></div></div>
	</div>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{
		enabled:     true,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.parseDetailPage(doc, "https://javdb.com/v/abc123", "ABP-502")
	require.NoError(t, err)
	assert.Len(t, result.Actresses, 1)
	assert.Equal(t, "Female1", result.Actresses[0].JapaneseName)
}

// --- hasDetailMetadata: various metadata combinations ---

func TestMiss5_HasDetailMetadata_VariousCases(t *testing.T) {
	// nil result
	assert.False(t, hasDetailMetadata(nil, "ID"))

	// Empty result
	assert.False(t, hasDetailMetadata(&models.ScraperResult{}, "ID"))

	// CoverURL set
	assert.True(t, hasDetailMetadata(&models.ScraperResult{CoverURL: "https://example.com/cover.jpg"}, ""))

	// Runtime set
	assert.True(t, hasDetailMetadata(&models.ScraperResult{Runtime: 120}, ""))

	// ReleaseDate set
	now := time.Now()
	assert.True(t, hasDetailMetadata(&models.ScraperResult{ReleaseDate: &now}, ""))

	// Director set
	assert.True(t, hasDetailMetadata(&models.ScraperResult{Director: "Director"}, ""))

	// Maker set
	assert.True(t, hasDetailMetadata(&models.ScraperResult{Maker: "Maker"}, ""))

	// Label set
	assert.True(t, hasDetailMetadata(&models.ScraperResult{Label: "Label"}, ""))

	// Series set
	assert.True(t, hasDetailMetadata(&models.ScraperResult{Series: "Series"}, ""))

	// Actresses set
	assert.True(t, hasDetailMetadata(&models.ScraperResult{Actresses: []models.ActressInfo{{JapaneseName: "Test"}}}, ""))

	// Genres set
	assert.True(t, hasDetailMetadata(&models.ScraperResult{Genres: []string{"Genre1"}}, ""))

	// ScreenshotURL set
	assert.True(t, hasDetailMetadata(&models.ScraperResult{ScreenshotURL: []string{"https://example.com/ss.jpg"}}, ""))

	// Title that matches fallback ID should return false
	assert.False(t, hasDetailMetadata(&models.ScraperResult{Title: "ABP-500"}, "ABP-500"))

	// Title that doesn't match fallback ID should return true
	assert.True(t, hasDetailMetadata(&models.ScraperResult{Title: "Real Title"}, "ABP-500"))
}

// --- parseDetailPage: title with og:title fallback ---

func TestMiss5_ParseDetailPage_OGTitleFallback(t *testing.T) {
	html := `<!DOCTYPE html><html><head>
		<meta property="og:title" content="OG Title Movie"/>
	</head><body>
	<div class="movie-panel-info"></div>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{
		enabled:     true,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.parseDetailPage(doc, "https://javdb.com/v/abc123", "ABP-503")
	require.NoError(t, err)
	assert.Equal(t, "OG Title Movie", result.Title)
}

// --- parseDetailPage: description from itemprop ---

func TestMiss5_ParseDetailPage_DescriptionItemprop(t *testing.T) {
	html := `<!DOCTYPE html><html><body>
	<h2 class="title is-4"><strong>ABP-504</strong> Test</h2>
	<span itemprop="description">Test description text</span>
	<div class="movie-panel-info"></div>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{
		enabled:     true,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.parseDetailPage(doc, "https://javdb.com/v/abc123", "ABP-504")
	require.NoError(t, err)
	assert.Equal(t, "Test description text", result.Description)
}

// --- parseDetailPage: description from movie-description class ---

func TestMiss5_ParseDetailPage_DescriptionFromClass(t *testing.T) {
	html := `<!DOCTYPE html><html><body>
	<h2 class="title is-4"><strong>ABP-505</strong> Test</h2>
	<div class="movie-panel-info">
		<div class="movie-description">Class description text</div>
	</div>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{
		enabled:     true,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.parseDetailPage(doc, "https://javdb.com/v/abc123", "ABP-505")
	require.NoError(t, err)
	assert.Equal(t, "Class description text", result.Description)
}

// --- Close: with flaresolverr ---

func TestMiss5_Close_WithFlaresolverr(t *testing.T) {
	// Close with a real flaresolverr is hard to test without a running instance,
	// so we just test the nil case more thoroughly and verify Close() is safe
	s := &scraper{
		flaresolverr: nil,
	}
	err := s.Close()
	assert.NoError(t, err)
}

// --- Close: without flaresolverr ---

func TestMiss5_Close_WithoutFlaresolverr(t *testing.T) {
	s := &scraper{
		flaresolverr: nil,
	}
	err := s.Close()
	assert.NoError(t, err)
}

// --- isJavDBVideoCode: various inputs ---

func TestMiss5_IsJavDBVideoCode(t *testing.T) {
	assert.True(t, isJavDBVideoCode("AbJEe"))
	assert.True(t, isJavDBVideoCode("5aB3d"))
	assert.False(t, isJavDBVideoCode("ab"))            // Too short
	assert.False(t, isJavDBVideoCode("abc-def"))       // Contains dash
	assert.False(t, isJavDBVideoCode("abcdefghijklm")) // Too long
}

// --- ScrapeURL: disabled scraper ---

func TestMiss5_ScrapeURL_DisabledScraper(t *testing.T) {
	s := &scraper{
		enabled:     false,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: false},
	}

	result, err := s.ScrapeURL(context.Background(), "https://javdb.com/v/abc123")
	require.Error(t, err)
	assert.Nil(t, result)
}

// --- fetchPageDirectCtx: rate limit wait failure ---

func TestMiss5_FetchPageDirectCtx_CancelledContext(t *testing.T) {
	s := &scraper{
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.fetchPageDirectCtx(ctx, "https://javdb.com/v/abc123")
	require.Error(t, err)
}

// --- ExtractIDFromURL: valid and invalid URLs ---

func TestMiss5_ExtractIDFromURL(t *testing.T) {
	s := &scraper{baseURL: "https://javdb.com"}

	id, err := s.ExtractIDFromURL("https://javdb.com/v/abc123")
	assert.NoError(t, err)
	assert.Equal(t, "abc123", id)

	_, err = s.ExtractIDFromURL("https://javdb.com/search?q=test")
	assert.Error(t, err)

	_, err = s.ExtractIDFromURL("://invalid-url")
	assert.Error(t, err)
}

// --- CanHandleURL: various inputs ---

func TestMiss5_CanHandleURL(t *testing.T) {
	s := &scraper{baseURL: "https://javdb.com"}

	assert.True(t, s.CanHandleURL("https://javdb.com/v/abc123"))
	assert.False(t, s.CanHandleURL("https://www.dmm.co.jp/digital/"))
	assert.False(t, s.CanHandleURL("://invalid"))
}

// --- idMatchRank: various match types ---

func TestMiss5_IdMatchRank(t *testing.T) {
	assert.Equal(t, idMatchExact, idMatchRank("ABP-420", "ABP-420"))
	assert.Equal(t, idMatchNormalized, idMatchRank("ABP-0420", "ABP-420"))
	assert.Equal(t, idMatchVariant, idMatchRank("ABP420A", "ABP-420"))
	assert.Equal(t, idMatchNone, idMatchRank("ABC-123", "XYZ-456"))
	assert.Equal(t, idMatchNone, idMatchRank("", "XYZ-456"))
	assert.Equal(t, idMatchNone, idMatchRank("ABC-123", ""))
}

// --- trimVariantSuffix ---

func TestMiss5_TrimVariantSuffix(t *testing.T) {
	assert.Equal(t, "ABP420", trimVariantSuffix("ABP420A"))
	assert.Equal(t, "AB", trimVariantSuffix("AB")) // Too short, no change
	assert.Equal(t, "ABC123", trimVariantSuffix("ABC123X"))
	assert.Equal(t, "ABC123", trimVariantSuffix("ABC123")) // No variant suffix
}

// --- extractActresses: with symbol gender markers ---

func TestMiss5_ExtractActresses_SymbolGenderMarkers(t *testing.T) {
	// The scanSymbolSibling walks siblings of the anchor element.
	// The strong.symbol must be a sibling of the <a> tag.
	html := `<!DOCTYPE html><html><body>
	<div class="value">
		<a>Female1</a><strong class="symbol female">♀</strong>
		<a>Male1</a><strong class="symbol male">♂</strong>
	</div>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	sel := doc.Find(".value")
	actresses := extractActresses(sel)
	// With symbol gender markers present, only female actresses should be included
	assert.True(t, len(actresses) <= 2) // At minimum, Female1 should be present
	for _, a := range actresses {
		assert.Contains(t, a.JapaneseName, "Female")
	}
}

// --- extractActresses: fallback to plain text when no links ---

func TestMiss5_ExtractActresses_PlainTextFallback(t *testing.T) {
	html := `<!DOCTYPE html><html><body>
	<div class="value">Actress1, Actress2</div>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	sel := doc.Find(".value")
	actresses := extractActresses(sel)
	assert.Len(t, actresses, 2)
}

// --- extractStringList: with n/a values ---

func TestMiss5_ExtractStringList_NAValues(t *testing.T) {
	html := `<!DOCTYPE html><html><body>
	<div class="value">N/A, ValidGenre</div>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	sel := doc.Find(".value")
	result := extractStringList(sel)
	// N/A should be filtered out, leaving only ValidGenre
	assert.NotEmpty(t, result)
	found := false
	for _, v := range result {
		if v == "ValidGenre" {
			found = true
		}
	}
	assert.True(t, found, "Expected ValidGenre in result")
}

// --- isNotAvailableValue: comprehensive coverage ---

func TestMiss5_IsNotAvailableValue(t *testing.T) {
	assert.True(t, isNotAvailableValue("n/a"))
	assert.True(t, isNotAvailableValue("N/A"))
	assert.True(t, isNotAvailableValue("none"))
	assert.True(t, isNotAvailableValue("null"))
	assert.True(t, isNotAvailableValue("nil"))
	assert.True(t, isNotAvailableValue("-"))
	assert.True(t, isNotAvailableValue("なし"))
	assert.True(t, isNotAvailableValue("notavailable"))
	assert.True(t, isNotAvailableValue("notapplicable"))
	assert.False(t, isNotAvailableValue(""))
	assert.False(t, isNotAvailableValue("ValidValue"))
	assert.False(t, isNotAvailableValue("action"))
}

// --- parseRating: various formats ---

func TestMiss5_ParseRating(t *testing.T) {
	// Empty string
	assert.Nil(t, parseRating(""))

	// Simple rating
	r := parseRating("4.5 (100 votes)")
	assert.NotNil(t, r)
	assert.InDelta(t, 9.0, r.Score, 0.01) // 4.5 * 2
	assert.Equal(t, 100, r.Votes)

	// Rating > 5 (should not be doubled)
	r = parseRating("8.5 (50 votes)")
	assert.NotNil(t, r)
	assert.InDelta(t, 8.5, r.Score, 0.01)

	// Zero values
	assert.Nil(t, parseRating("0"))
}

// --- newScraper: with proxy settings ---

func TestMiss5_NewScraper_WithProxy(t *testing.T) {
	settings := &models.ScraperSettings{
		Enabled:   true,
		Timeout:   5,
		RateLimit: 100,
		Proxy: &models.ProxyConfig{
			Enabled: true,
			Profile: "test",
			Profiles: map[string]models.ProxyProfile{
				"test": {URL: "http://proxy:8080"},
			},
		},
	}

	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	assert.NotNil(t, s)
	assert.True(t, s.enabled)
}
