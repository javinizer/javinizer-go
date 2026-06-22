package libredmm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScrapeURLV4_Disabled(t *testing.T) {
	s := &scraper{
		client:          resty.New(),
		enabled:         false,
		baseURL:         "https://www.libredmm.com",
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: false},
	}

	result, err := s.ScrapeURL(context.Background(), "https://www.libredmm.com/movies/ABC-123")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestScrapeURLV4_NotHandled(t *testing.T) {
	s := &scraper{
		client:          resty.New(),
		enabled:         true,
		baseURL:         "https://www.libredmm.com",
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: true},
	}

	result, err := s.ScrapeURL(context.Background(), "https://example.com/movies/ABC-123")
	assert.Nil(t, result)
	assert.Error(t, err)
}

func TestSearchV4_Disabled(t *testing.T) {
	s := &scraper{
		client:          resty.New(),
		enabled:         false,
		baseURL:         "https://www.libredmm.com",
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: false},
	}

	result, err := s.Search(context.Background(), "ABC-123")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestSearchV4_Success(t *testing.T) {
	payload := moviePayload{
		NormalizedID:  "ABC-123",
		Title:         "Search Movie",
		Date:          "2024-03-20",
		CoverImageURL: "https://pics.dmm.co.jp/cover2.jpg",
		Makers:        []string{"Studio X"},
		Genres:        []string{"Action"},
	}
	payloadBytes, _ := json.Marshal(payload)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write(payloadBytes)
	}))
	defer ts.Close()

	s := &scraper{
		client:          resty.New().SetBaseURL(ts.URL),
		enabled:         true,
		baseURL:         ts.URL,
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: true},
	}

	result, err := s.Search(context.Background(), "ABC-123")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "ABC-123", result.ID)
	assert.Equal(t, "Search Movie", result.Title)
}

func TestSearchV4_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer ts.Close()

	s := &scraper{
		client:          resty.New().SetBaseURL(ts.URL),
		enabled:         true,
		baseURL:         ts.URL,
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: true},
	}

	result, err := s.Search(context.Background(), "ABC-123")
	assert.Nil(t, result)
	assert.Error(t, err)
}

func TestCanHandleURLV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	assert.True(t, s.CanHandleURL("https://www.libredmm.com/movies/ABC-123"))
	assert.True(t, s.CanHandleURL("https://libredmm.com/movies/ABC-123"))
	assert.False(t, s.CanHandleURL("https://example.com/movies/ABC-123"))
	assert.False(t, s.CanHandleURL(""))
}

func TestExtractIDFromURLV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	id, err := s.ExtractIDFromURL("https://www.libredmm.com/movies/ABC-123")
	assert.NoError(t, err)
	assert.Equal(t, "ABC-123", id)

	id, err = s.ExtractIDFromURL("https://www.libredmm.com/movies/ABC-123.json")
	assert.NoError(t, err)
	assert.Equal(t, "ABC-123", id)

	id, err = s.ExtractIDFromURL("https://www.libredmm.com/?q=ABC-123")
	assert.NoError(t, err)
	assert.Equal(t, "ABC-123", id)
}

func TestStripJSONSuffixV4(t *testing.T) {
	assert.Equal(t, "ABC-123", stripJSONSuffix("ABC-123.json"))
	assert.Equal(t, "ABC-123", stripJSONSuffix("ABC-123"))
}
