package javstash

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Search HTTP 401 Unauthorized ---

func TestScraper_Search_HTTPUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"error":"unauthorized"}`)
	}))
	defer server.Close()

	s := &scraper{
		enabled:     true,
		apiKey:      "test-api-key",
		baseURL:     server.URL,
		client:      NewTestClient(server),
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{},
	}

	_, err := s.Search(context.Background(), "IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid API key")
}

// --- Search non-200 status ---

func TestScraper_Search_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `Internal Server Error`)
	}))
	defer server.Close()

	s := &scraper{
		enabled:     true,
		apiKey:      "test-api-key",
		baseURL:     server.URL,
		client:      NewTestClient(server),
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{},
	}

	_, err := s.Search(context.Background(), "IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
}

// --- Search invalid JSON response ---

func TestScraper_Search_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `this is not json`)
	}))
	defer server.Close()

	s := &scraper{
		enabled:     true,
		apiKey:      "test-api-key",
		baseURL:     server.URL,
		client:      NewTestClient(server),
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{},
	}

	_, err := s.Search(context.Background(), "IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse response")
}

// --- Search GraphQL error (not "Not authorized") ---

func TestScraper_Search_GraphQLError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"errors":[{"message":"Something went wrong"}]}`)
	}))
	defer server.Close()

	s := &scraper{
		enabled:     true,
		apiKey:      "test-api-key",
		baseURL:     server.URL,
		client:      NewTestClient(server),
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{},
	}

	_, err := s.Search(context.Background(), "IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GraphQL error")
}

// --- parseScene: no code, no images, no URLs ---

func TestParseScene_NoCode_NoImages_NoURLs(t *testing.T) {
	s := &scraper{
		baseURL:  "https://javstash.org/graphql",
		language: "en",
	}

	sc := &scene{
		ID:          "scene123",
		Code:        "",
		Title:       "Movie Without Code",
		ReleaseDate: "2024-01-15",
		Duration:    120,
		Director:    "Test Director",
		Details:     "Test details",
		Studio:      &studio{ID: "s1", Name: "Test Studio"},
		Performers: []performer{{Performer: struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}{ID: "p1", Name: "Actress Name"}}},
		Tags:   []tag{{ID: "t1", Name: "Tag1"}},
		Images: nil,
		URLs:   nil,
	}

	result, err := s.parseScene(sc, "SEARCH-123")
	require.NoError(t, err)
	assert.Equal(t, "javstash", result.Source)
	assert.Equal(t, "SEARCH-123", result.ID)
	assert.Equal(t, "SEARCH-123", result.ContentID) // fallback when no code
	assert.Equal(t, "Movie Without Code", result.Title)
	assert.Equal(t, "Test Studio", result.Maker)
	assert.Equal(t, 2, result.Runtime) // 120/60
	assert.Len(t, result.Actresses, 1)
	assert.Len(t, result.Genres, 1)
	assert.Empty(t, result.CoverURL)  // no images
	assert.Empty(t, result.PosterURL) // no images
	// SourceURL is always set from the scene ID
	assert.Contains(t, result.SourceURL, "scene123")
	assert.NotNil(t, result.ReleaseDate)
}

// --- parseScene: with poster/cover image and DMM URLs ---

func TestParseScene_WithImagesAndURLs(t *testing.T) {
	s := &scraper{
		baseURL:  "https://javstash.org/graphql",
		language: "ja",
	}

	sc := &scene{
		ID:          "scene456",
		Code:        "IPX-535",
		Title:       "Test Movie",
		ReleaseDate: "2024-03-20",
		Duration:    90,
		Studio:      nil,
		Performers:  nil,
		Tags:        nil,
		Images: []image{
			{ID: "i1", URL: "https://example.com/poster.jpg"},
			{ID: "i2", URL: "https://example.com/cover.jpg"},
		},
		URLs: []urlEntry{
			{URL: "https://www.dmm.co.jp/digital/video/-/detail/=/cid=ipx00535/"},
			{URL: "https://javstash.org/scenes/scene456"},
		},
	}

	result, err := s.parseScene(sc, "IPX-535")
	require.NoError(t, err)
	assert.Equal(t, "ipx00535", result.ContentID)
	assert.Equal(t, "https://www.dmm.co.jp/digital/video/-/detail/=/cid=ipx00535/", result.SourceURL)
	assert.NotEmpty(t, result.PosterURL)
	assert.NotEmpty(t, result.CoverURL)
	assert.Equal(t, "ja", result.Language)
}

// --- parseScene: images with poster keyword ---

func TestParseScene_ImagesWithPosterKeyword(t *testing.T) {
	s := &scraper{
		baseURL:  "https://javstash.org/graphql",
		language: "en",
	}

	sc := &scene{
		ID:    "scene789",
		Code:  "ABC-123",
		Title: "Movie",
		Images: []image{
			{ID: "i1", URL: "https://example.com/poster_image.jpg"},
			{ID: "i2", URL: "https://example.com/regular_image.jpg"},
		},
		URLs: nil,
	}

	result, err := s.parseScene(sc, "ABC-123")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/poster_image.jpg", result.PosterURL)
	assert.Equal(t, "https://example.com/poster_image.jpg", result.CoverURL)
}

// --- parseScene: no DMM cid URLs ---

func TestParseScene_NonDMMURLs(t *testing.T) {
	s := &scraper{
		baseURL:  "https://javstash.org/graphql",
		language: "en",
	}

	sc := &scene{
		ID:     "scene000",
		Code:   "XYZ-999",
		Title:  "Movie",
		Images: nil,
		URLs: []urlEntry{
			{URL: "https://javstash.org/scenes/scene000"},
			{URL: "https://example.com/other"},
		},
	}

	result, err := s.parseScene(sc, "XYZ-999")
	require.NoError(t, err)
	assert.Equal(t, "XYZ-999", result.ContentID)                              // code is set
	assert.Equal(t, "https://javstash.org/scenes/scene000", result.SourceURL) // fallback to first URL
}

// --- GetURL with valid ID ---

func TestScraper_GetURL_ValidID(t *testing.T) {
	s := &scraper{
		baseURL:     "https://javstash.org/graphql",
		rateLimiter: ratelimit.NewLimiter(0),
	}

	url, err := s.GetURL(context.Background(), "scene123")
	require.NoError(t, err)
	assert.Equal(t, "https://javstash.org/scenes/scene123", url)
}

// --- GetURL with whitespace-only ID ---

func TestScraper_GetURL_WhitespaceOnly(t *testing.T) {
	s := &scraper{
		baseURL:     "https://javstash.org/graphql",
		rateLimiter: ratelimit.NewLimiter(0),
	}

	_, err := s.GetURL(context.Background(), "   ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")
}

// --- ResolveDownloadProxyForHost ---

func TestScraper_ResolveDownloadProxyForHost(t *testing.T) {
	s := &scraper{rateLimiter: ratelimit.NewLimiter(0)}
	_, _, ok := s.ResolveDownloadProxyForHost("any-host.com")
	assert.False(t, ok, "JAVStash should not claim any hosts")
}

// --- Config method ---

func TestScraper_Config_MissTest(t *testing.T) {
	settings := models.ScraperSettings{Enabled: true, Timeout: 30}
	s := &scraper{
		settings:    settings,
		rateLimiter: ratelimit.NewLimiter(0),
	}
	cfg := s.Config()
	require.NotNil(t, cfg)
	assert.True(t, cfg.Enabled)
	assert.Equal(t, 30, cfg.Timeout)
	// Verify it's a clone
	cfg.Enabled = false
	assert.True(t, s.Config().Enabled)
}

// --- Close method ---

func TestScraper_Close_MissTest(t *testing.T) {
	s := &scraper{rateLimiter: ratelimit.NewLimiter(0)}
	assert.NoError(t, s.Close())
}

// --- Search rate limit wait failure (context cancellation) ---
// This tests the rate limiter Wait path, which errors when context is cancelled

func TestScraper_Search_RateLimitContextCancelled(t *testing.T) {
	// Use a long rate limit so context times out before Wait returns
	s := &scraper{
		enabled:     true,
		apiKey:      "test-key",
		baseURL:     "https://javstash.org/graphql",
		client:      resty.New(),
		rateLimiter: ratelimit.NewLimiter(10 * time.Second),
		settings:    models.ScraperSettings{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := s.Search(ctx, "IPX-535")
	require.Error(t, err)
}

// --- Search with full scene result covering all fields ---

func TestScraper_Search_FullSceneResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-api-key", r.Header.Get("ApiKey"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"data": {
				"searchScene": [{
					"id": "abc123",
					"code": "SSIS-001",
					"title": "Full Scene Test",
					"release_date": "2024-06-01",
					"duration": 180,
					"director": "Test Director",
					"details": "Full description of the scene",
					"studio": {"id": "s1", "name": "S1 Studio"},
					"performers": [
						{"performer": {"id": "p1", "name": "Actress A"}},
						{"performer": {"id": "p2", "name": "Actress B"}}
					],
					"tags": [{"id": "t1", "name": "Tag1"}],
					"images": [{"id": "i1", "url": "https://example.com/cover_image.jpg"}],
					"urls": [{"url": "https://example.com/source"}]
				}]
			}
		}`))
	}))
	defer server.Close()

	s := &scraper{
		enabled:     true,
		apiKey:      "test-api-key",
		baseURL:     server.URL,
		client:      NewTestClient(server),
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{},
	}

	result, err := s.Search(context.Background(), "SSIS-001")
	require.NoError(t, err)
	assert.Equal(t, "javstash", result.Source)
	assert.Equal(t, "SSIS-001", result.ContentID)
	assert.Equal(t, "Full Scene Test", result.Title)
	assert.Equal(t, 3, result.Runtime) // 180/60
	assert.Equal(t, "Test Director", result.Director)
	assert.Equal(t, "Full description of the scene", result.Description)
	assert.Equal(t, "S1 Studio", result.Maker)
	assert.Len(t, result.Actresses, 2)
	assert.Len(t, result.Genres, 1)
	assert.Equal(t, "https://example.com/cover_image.jpg", result.CoverURL)
}
