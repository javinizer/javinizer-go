package javstash

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisteredJAVStashURLAdmission(t *testing.T) {
	const apiKey = "fixture-api-key"
	for _, tc := range []struct {
		name, baseURL, wantErr           string
		disabled, missingKey, parseError bool
	}{
		{name: "malformed path escape", baseURL: "https://javstash.org/graph%zz", wantErr: "javstash.base_url must be a valid URL:", parseError: true},
		{name: "lookalike host", baseURL: "https://javstash.org.evil.invalid/graphql", wantErr: `javstash.base_url host "javstash.org.evil.invalid" is not in the allowed list`},
		{name: "loopback host", baseURL: "http://127.0.0.1/graphql", wantErr: `javstash.base_url host "127.0.0.1" is not in the allowed list`},
		{name: "canonical endpoint", baseURL: "https://javstash.org/graphql"},
		{name: "www endpoint", baseURL: "https://www.javstash.org/graphql"},
		{name: "registered default"},
		{name: "missing key precedes URL validation", baseURL: "https://javstash.org/graph%zz", missingKey: true, wantErr: "javstash: api_key is required (set in config)"},
		{name: "disabled module is not admitted for use", baseURL: "https://javstash.org/graph%zz", disabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := scraperutil.NewScraperRegistry()
			Register(registry)
			defaults := registry.GetAllDefaults()
			require.Equal(t, defaultBaseURL, defaults["javstash"].BaseURL)
			override := models.ScraperSettings{Enabled: !tc.disabled, APIKey: apiKey, BaseURL: tc.baseURL, Timeout: 30, Cookies: map[string]string{"fixture": "preserve-me"}}
			if tc.missingKey {
				override.APIKey = ""
			}
			original := override.Clone()
			cfg := &config.Config{Scrapers: config.ScrapersConfig{Overrides: map[string]*models.ScraperSettings{"javstash": &override}}}
			require.NoError(t, cfg.Scrapers.Finalize(registry))
			resolved := cfg.Scrapers.ResolvedSettings("javstash")
			wantURL := tc.baseURL
			if wantURL == "" {
				wantURL = defaultBaseURL
			}
			require.Equal(t, wantURL, resolved.BaseURL)
			var firstError string
			for attempt := range 2 {
				err := config.ValidateScraperOverrides(cfg)
				if tc.wantErr == "" {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, tc.wantErr)
					assert.NotContains(t, err.Error(), apiKey)
					if attempt == 0 {
						firstError = err.Error()
					} else {
						assert.Equal(t, firstError, err.Error())
					}
					var parseErr *url.Error
					if tc.parseError {
						require.ErrorAs(t, err, &parseErr)
						assert.Equal(t, "parse", parseErr.Op)
						assert.Equal(t, tc.baseURL, parseErr.URL)
						var escapeErr url.EscapeError
						require.ErrorAs(t, err, &escapeErr)
						assert.Equal(t, url.EscapeError("%zz"), escapeErr)
					} else {
						assert.False(t, errors.As(err, &parseErr))
					}
					var scraperErr *models.ScraperError
					assert.False(t, errors.As(err, &scraperErr))
				}
				assert.Same(t, &override, cfg.Scrapers.Overrides["javstash"])
				assert.Equal(t, original, override)
				assert.Equal(t, resolved, cfg.Scrapers.ResolvedSettings("javstash"))
				assert.Equal(t, defaults, registry.GetAllDefaults())
				assert.Empty(t, registry.GetAllInstances(), "validation must not construct a scraper")
			}
		})
	}
}

type urlBoundaryDenyTransport struct{ calls atomic.Int32 }

func (d *urlBoundaryDenyTransport) RoundTrip(*http.Request) (*http.Response, error) {
	d.calls.Add(1)
	return nil, errors.New("URL boundary test forbids HTTP requests")
}

func TestJAVStashURLHandlerRejectsMalformedPathWithoutIO(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	Register(registry)
	override := &models.ScraperSettings{Enabled: true, APIKey: "fixture-api-key", Timeout: 30}
	cfg := &config.Config{Scrapers: config.ScrapersConfig{Overrides: map[string]*models.ScraperSettings{"javstash": override}}}
	require.NoError(t, cfg.Scrapers.Finalize(registry))
	require.NoError(t, config.ValidateScraperOverrides(cfg))
	constructor, ok := registry.GetScraperConstructor("javstash")
	require.True(t, ok)
	instance, err := constructor(scraperutil.ScraperDeps{Settings: cfg.Scrapers.ResolvedSettings("javstash")})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, instance.Close()) })
	handler, ok := instance.(models.URLHandler)
	require.True(t, ok)
	concrete, ok := instance.(*scraper)
	require.True(t, ok)
	deny := &urlBoundaryDenyTransport{}
	concrete.client.SetTransport(deny)
	before := instance.Config()
	validControl := func() {
		for _, tc := range []struct{ raw, id string }{
			{"https://javstash.org/scenes/scene123", "scene123"},
			{"https://www.javstash.org/scenes/scene%2D123", "scene-123"},
		} {
			assert.True(t, handler.CanHandleURL(tc.raw))
			id, err := handler.ExtractIDFromURL(tc.raw)
			require.NoError(t, err)
			assert.Equal(t, tc.id, id)
		}
	}
	validControl()
	const malformed = "https://javstash.org/scenes/scene%zz"
	for range 2 {
		assert.False(t, handler.CanHandleURL(malformed))
		id, err := handler.ExtractIDFromURL(malformed)
		assert.Empty(t, id)
		require.ErrorContains(t, err, "failed to parse URL:")
		var parseErr *url.Error
		require.ErrorAs(t, err, &parseErr)
		assert.Equal(t, "parse", parseErr.Op)
		assert.Equal(t, malformed, parseErr.URL)
		var escapeErr url.EscapeError
		require.ErrorAs(t, err, &escapeErr)
		assert.Equal(t, url.EscapeError("%zz"), escapeErr)
		var scraperErr *models.ScraperError
		assert.False(t, errors.As(err, &scraperErr))
		validControl()
		assert.Equal(t, before, instance.Config())
	}
	assert.False(t, handler.CanHandleURL("https://javstash.org.evil.invalid/scenes/scene123"))
	result, err := handler.ScrapeURL(context.Background(), "https://javstash.org/scenes/scene123")
	assert.Nil(t, result)
	var scraperErr *models.ScraperError
	require.ErrorAs(t, err, &scraperErr)
	assert.Equal(t, "JAVStash", scraperErr.Scraper)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
	assert.False(t, scraperErr.Retryable)
	assert.False(t, scraperErr.Temporary)
	assert.Contains(t, err.Error(), "does not support direct URL scraping")
	assert.Equal(t, before, instance.Config())
	assert.Zero(t, deny.calls.Load(), "URL handling must not send HTTP requests")
}
