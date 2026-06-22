package javstash

import (
	"context"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchV4_Disabled(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "http://localhost:9999",
		apiKey:      "test-key",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
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

	assert.True(t, s.CanHandleURL("https://javstash.org/scene/123"))
	assert.True(t, s.CanHandleURL("https://www.javstash.org/scene/123"))
	assert.False(t, s.CanHandleURL("https://localhost:9999/scene/123"))
	assert.False(t, s.CanHandleURL(""))
}

func TestConfigV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true, BaseURL: "http://localhost:9999"},
	}

	cfg := s.Config()
	require.NotNil(t, cfg)
	assert.Equal(t, "http://localhost:9999", cfg.BaseURL)
}

func TestCloseV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}
	assert.NoError(t, s.Close())
}

func TestResolveDownloadProxyForHostV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	_, _, ok := s.ResolveDownloadProxyForHost("javstash.org")
	assert.False(t, ok)

	_, _, ok = s.ResolveDownloadProxyForHost("")
	assert.False(t, ok)
}

func TestNewScraperV4(t *testing.T) {
	settings := &models.ScraperSettings{
		Enabled: true,
		BaseURL: "http://localhost:9999",
	}
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	require.NotNil(t, s)
	assert.True(t, s.IsEnabled())
	assert.Equal(t, "javstash", s.Name())
}
