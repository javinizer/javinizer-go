package javstash

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func NewTestClient(server *httptest.Server) *resty.Client {
	client := resty.New()
	client.SetTimeout(30 * time.Second)
	transport := &http.Transport{}
	transport.Proxy = http.ProxyURL(nil)
	client.SetTransport(transport)
	client.SetBaseURL(server.URL)
	return client
}

func TestScraper_Name(t *testing.T) {
	s := &Scraper{rateLimiter: ratelimit.NewLimiter(0)}
	assert.Equal(t, "javstash", s.Name())
}

func TestScraper_IsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		want    bool
	}{
		{"enabled", true, true},
		{"disabled", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Scraper{enabled: tt.enabled, rateLimiter: ratelimit.NewLimiter(0)}
			assert.Equal(t, tt.want, s.IsEnabled())
		})
	}
}

func TestScraper_ValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.ScraperSettings
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: true,
			errMsg:  "config is nil",
		},
		{
			name: "disabled",
			cfg: &config.ScraperSettings{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "enabled without api key",
			cfg: &config.ScraperSettings{
				Enabled: true,
			},
			wantErr: true,
			errMsg:  "api_key is required",
		},
		{
			name: "enabled with api key",
			cfg: &config.ScraperSettings{
				Enabled: true,
				Extra:   map[string]any{"api_key": "test-key"},
			},
			wantErr: false,
		},
		{
			name: "negative rate limit",
			cfg: &config.ScraperSettings{
				Enabled:   true,
				Extra:     map[string]any{"api_key": "test-key"},
				RateLimit: -1,
			},
			wantErr: true,
			errMsg:  "rate_limit must be non-negative",
		},
		{
			name: "negative retry count",
			cfg: &config.ScraperSettings{
				Enabled:    true,
				Extra:      map[string]any{"api_key": "test-key"},
				RetryCount: -1,
			},
			wantErr: true,
			errMsg:  "retry_count must be non-negative",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Scraper{rateLimiter: ratelimit.NewLimiter(0)}
			err := s.ValidateConfig(tt.cfg)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestScraper_Search_MissingAPIKey(t *testing.T) {
	s := &Scraper{
		enabled:     true,
		apiKey:      "",
		rateLimiter: ratelimit.NewLimiter(0),
	}
	_, err := s.Search(context.Background(), "IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api_key is required")
}

func TestScraper_Search_EmptyID(t *testing.T) {
	s := &Scraper{
		enabled:     true,
		apiKey:      "test-key",
		rateLimiter: ratelimit.NewLimiter(0),
	}
	_, err := s.Search(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id cannot be empty")

	_, err = s.Search(context.Background(), "   ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id cannot be empty")
}

func TestScraper_GetURL_EmptyID(t *testing.T) {
	s := &Scraper{
		enabled:     true,
		apiKey:      "test-key",
		rateLimiter: ratelimit.NewLimiter(0),
	}
	_, err := s.GetURL("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id cannot be empty")

	_, err = s.GetURL("   ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id cannot be empty")
}

func TestScraper_Search_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-api-key", r.Header.Get("ApiKey"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"data": {
				"searchScene": [{
					"id": "abc123",
					"code": "IPX-535",
					"title": "Test Movie Title",
					"release_date": "2023-01-15",
					"duration": 120,
					"director": "Test Director",
					"details": "Test description",
					"studio": {"id": "s1", "name": "Test Studio"},
					"performers": [{"performer": {"id": "p1", "name": "Actress Name"}}],
					"tags": [{"id": "t1", "name": "Tag1"}, {"id": "t2", "name": "Tag2"}],
					"images": [{"id": "i1", "url": "https://example.com/image.jpg"}],
					"urls": [
						{"url": "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=ipx00535/"},
						{"url": "https://javstash.org/scenes/abc123"}
					]
				}]
			}
		}`))
	}))
	defer server.Close()

	s := &Scraper{
		enabled:     true,
		apiKey:      "test-api-key",
		baseURL:     server.URL,
		client:      NewTestClient(server),
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    config.ScraperSettings{},
	}

	result, err := s.Search(context.Background(), "IPX-535")
	require.NoError(t, err)
	assert.Equal(t, "javstash", result.Source)
	assert.Equal(t, "Test Movie Title", result.Title)
	assert.Equal(t, 120, result.Runtime)
	assert.Equal(t, "Test Director", result.Director)
	assert.Equal(t, "Test description", result.Description)
	assert.Equal(t, "Test Studio", result.Maker)
	assert.Equal(t, "ipx00535", result.ContentID, "ContentID should be extracted from DMM URL")
	assert.Equal(t, "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=ipx00535/", result.SourceURL)
	assert.Len(t, result.Actresses, 1)
	assert.Equal(t, "Actress Name", result.Actresses[0].JapaneseName)
	assert.Len(t, result.Genres, 2)
	assert.Equal(t, "Tag1", result.Genres[0])
	assert.Equal(t, "Tag2", result.Genres[1])
}

func TestExtractDMMContentID(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "DMM mono URL",
			url:      "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=ipx00535/",
			expected: "ipx00535",
		},
		{
			name:     "DMM video URL with query",
			url:      "https://video.dmm.co.jp/av/content/?id=royd00191",
			expected: "",
		},
		{
			name:     "non-DMM URL",
			url:      "https://javstash.org/scenes/abc123",
			expected: "",
		},
		{
			name:     "DMM URL with trailing parameters",
			url:      "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=ssis00123/&foo=bar",
			expected: "ssis00123",
		},
		{
			name:     "empty URL",
			url:      "",
			expected: "",
		},
		{
			name:     "DMM URL without trailing slash",
			url:      "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=abw00102",
			expected: "abw00102",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDMMContentID(tt.url)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestScraper_Search_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"data": {
				"searchScene": []
			}
		}`))
	}))
	defer server.Close()

	s := &Scraper{
		enabled:     true,
		apiKey:      "test-api-key",
		baseURL:     server.URL,
		client:      NewTestClient(server),
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    config.ScraperSettings{},
	}

	_, err := s.Search(context.Background(), "NOTFOUND-999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no results")
}

func TestScraper_Search_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"errors": [{"message": "Not authorized"}]
		}`))
	}))
	defer server.Close()

	s := &Scraper{
		enabled:     true,
		apiKey:      "test-api-key",
		baseURL:     server.URL,
		client:      NewTestClient(server),
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    config.ScraperSettings{},
	}

	_, err := s.Search(context.Background(), "IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API key required")
}

func TestNormalizeLanguage(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"en", "en"},
		{"ja", "ja"},
		{"JA", "ja"},
		{"", "en"},
		{"other", "en"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, scraperutil.NormalizeLanguage(tt.input))
		})
	}
}

func TestCanHandleURL(t *testing.T) {
	s := &Scraper{baseURL: "https://javstash.org/graphql", rateLimiter: ratelimit.NewLimiter(0)}

	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		{"javstash.org", "https://javstash.org/scenes/abc123", true},
		{"www.javstash.org", "https://www.javstash.org/scenes/abc123", true},
		{"other site", "https://www.example.com/scenes/abc123", false},
		{"malformed URL", "not-a-url", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.CanHandleURL(tt.url)
			assert.Equal(t, tt.expected, got, "CanHandleURL(%q) = %v, want %v", tt.url, got, tt.expected)
		})
	}
}

func TestExtractIDFromURL(t *testing.T) {
	s := &Scraper{baseURL: "https://javstash.org/graphql", rateLimiter: ratelimit.NewLimiter(0)}

	tests := []struct {
		name     string
		url      string
		expected string
		wantErr  bool
	}{
		{"standard path", "https://javstash.org/scenes/abc123", "abc123", false},
		{"with trailing slash", "https://javstash.org/scenes/abc123/", "abc123", false},
		{"nested path", "https://javstash.org/en/scenes/def456", "def456", false},
		{"empty path", "https://javstash.org/", "", true},
		{"short segment", "https://javstash.org/a", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ExtractIDFromURL(tt.url)
			if tt.wantErr {
				assert.Error(t, err, "ExtractIDFromURL(%q) expected error, got nil", tt.url)
			} else {
				assert.NoError(t, err, "ExtractIDFromURL(%q) unexpected error: %v", tt.url, err)
				assert.Equal(t, tt.expected, got, "ExtractIDFromURL(%q) = %q, want %q", tt.url, got, tt.expected)
			}
		})
	}
}

func TestScrapeURL_ReturnsNotSupported(t *testing.T) {
	s := &Scraper{enabled: true, apiKey: "test-key", rateLimiter: ratelimit.NewLimiter(0)}

	_, err := s.ScrapeURL(context.Background(), "https://javstash.org/scenes/abc123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support direct URL scraping")
}

func TestScraperInterfaceCompliance_JavStash(t *testing.T) {
	s := &Scraper{rateLimiter: ratelimit.NewLimiter(0)}
	var _ models.Scraper = s
	var _ models.URLHandler = s
	var _ models.DirectURLScraper = s
}
