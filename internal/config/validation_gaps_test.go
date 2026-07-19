package config

import (
	"encoding/json"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validateFnTestResolver is a resolver whose GetValidateFn returns a real
// closure for a configured scraper name, so ValidateScraperOverrides can
// exercise the scraper-specific validation dispatch path.
type validateFnTestResolver struct {
	staticTestConfigResolver
	validateFns map[string]func(*models.ScraperSettings) error
}

func (r *validateFnTestResolver) GetValidateFn(name string) func(*models.ScraperSettings) error {
	if fn, ok := r.validateFns[name]; ok {
		return fn
	}
	return nil
}

// --- validateHTTPBaseURL -----------------------------------------------------

func TestValidateHTTPBaseURL_URLParseError(t *testing.T) {
	// url.Parse returns an error for an unbalanced IPv6 literal host.
	err := validateHTTPBaseURL("some.path", "http://[::1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a valid http(s) URL")
}

// --- ValidateScraperOverrides ------------------------------------------------

func TestValidateScraperOverrides_NilConfig(t *testing.T) {
	assert.NoError(t, ValidateScraperOverrides(nil))
}

func TestValidateScraperOverrides_NilOverridesNormalizes(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Finalize(NewTestScraperConfigResolverInterface())
	// Force Overrides to nil so the nil-guard Normalize path runs.
	cfg.Scrapers.Overrides = nil
	assert.NoError(t, ValidateScraperOverrides(cfg))
	// Normalize should have populated Overrides.
	assert.NotNil(t, cfg.Scrapers.Overrides)
}

func TestValidateScraperOverrides_NegativeRateLimit(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Finalize(NewTestScraperConfigResolverInterface())
	cfg.Scrapers.Override("r18dev").Enabled = true
	cfg.Scrapers.Override("r18dev").RateLimit = -1
	err := ValidateScraperOverrides(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate_limit")
}

func TestValidateScraperOverrides_NegativeRetryCount(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Finalize(NewTestScraperConfigResolverInterface())
	cfg.Scrapers.Override("r18dev").Enabled = true
	cfg.Scrapers.Override("r18dev").RetryCount = -3
	err := ValidateScraperOverrides(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "retry_count")
}

func TestValidateScraperOverrides_NegativeTimeout(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Finalize(NewTestScraperConfigResolverInterface())
	cfg.Scrapers.Override("r18dev").Enabled = true
	cfg.Scrapers.Override("r18dev").Timeout = -5
	err := ValidateScraperOverrides(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}

func TestValidateScraperOverrides_NilSettingsSkipped(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Finalize(NewTestScraperConfigResolverInterface())
	// Inject a nil entry directly to exercise the sc.Validate nil guard.
	cfg.Scrapers.Overrides["r18dev"] = nil
	err := ValidateScraperOverrides(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config is nil")
}

func TestValidateScraperOverrides_DisabledSkipsValidateFn(t *testing.T) {
	// A disabled scraper with a failing validateFn must NOT error — disabled
	// scrapers skip scraper-specific validation.
	failing := func(*models.ScraperSettings) error {
		return assertError("should not be called")
	}
	resolver := &validateFnTestResolver{
		staticTestConfigResolver: staticTestConfigResolver{
			registered: map[string]bool{"r18dev": true},
			defaults:   map[string]models.ScraperSettings{"r18dev": {Enabled: false}},
		},
		validateFns: map[string]func(*models.ScraperSettings) error{"r18dev": failing},
	}
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Finalize(resolver)
	cfg.Scrapers.Override("r18dev").Enabled = false
	assert.NoError(t, ValidateScraperOverrides(cfg))
}

func TestValidateScraperOverrides_ValidateFnError(t *testing.T) {
	want := assertError("scraper-specific validation failure")
	resolver := &validateFnTestResolver{
		staticTestConfigResolver: staticTestConfigResolver{
			registered: map[string]bool{"r18dev": true},
			defaults:   map[string]models.ScraperSettings{"r18dev": {Enabled: true}},
		},
		validateFns: map[string]func(*models.ScraperSettings) error{
			"r18dev": func(*models.ScraperSettings) error { return want },
		},
	}
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Finalize(resolver)
	cfg.Scrapers.Override("r18dev").Enabled = true
	// Re-normalize so the validateFn dispatch is rebuilt for the newly-added override.
	cfg.Scrapers.Normalize()
	err := ValidateScraperOverrides(cfg)
	require.Error(t, err)
	assert.Equal(t, want, err)
}

// assertError is a tiny helper to build a sentinel error inline.
func assertError(msg string) error {
	return &validationGapError{msg: msg}
}

type validationGapError struct{ msg string }

func (e *validationGapError) Error() string { return e.msg }

// --- UnmarshalJSON error paths ----------------------------------------------

func TestScrapersConfigUnmarshalJSON_InvalidJSON(t *testing.T) {
	var sc ScrapersConfig
	// A JSON array is a valid JSON value but cannot decode into a map, so it
	// reaches the inner json.Unmarshal(data, &raw) call and fails there.
	err := json.Unmarshal([]byte("[]"), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal scrapers config")
}

func TestScrapersConfigUnmarshalJSON_UserAgentWrongType(t *testing.T) {
	var sc ScrapersConfig
	err := json.Unmarshal([]byte(`{"user_agent": 123}`), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user_agent must be a string")
}

func TestScrapersConfigUnmarshalJSON_RefererWrongType(t *testing.T) {
	var sc ScrapersConfig
	err := json.Unmarshal([]byte(`{"referer": 123}`), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "referer must be a string")
}

func TestScrapersConfigUnmarshalJSON_TimeoutSecondsWrongType(t *testing.T) {
	var sc ScrapersConfig
	err := json.Unmarshal([]byte(`{"timeout_seconds": "nope"}`), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout_seconds must be an integer")
}

func TestScrapersConfigUnmarshalJSON_RequestTimeoutSecondsWrongType(t *testing.T) {
	var sc ScrapersConfig
	err := json.Unmarshal([]byte(`{"request_timeout_seconds": "nope"}`), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request_timeout_seconds must be an integer")
}

func TestScrapersConfigUnmarshalJSON_PriorityWrongType(t *testing.T) {
	var sc ScrapersConfig
	err := json.Unmarshal([]byte(`{"priority": "not-an-array"}`), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "priority must be an array of strings")
}

func TestScrapersConfigUnmarshalJSON_ProxyWrongType(t *testing.T) {
	var sc ScrapersConfig
	err := json.Unmarshal([]byte(`{"proxy": "not-an-object"}`), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal proxy")
}

func TestScrapersConfigUnmarshalJSON_FlareSolverrWrongType(t *testing.T) {
	var sc ScrapersConfig
	err := json.Unmarshal([]byte(`{"flaresolverr": "not-an-object"}`), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal flaresolverr")
}

func TestScrapersConfigUnmarshalJSON_ScrapeActressWrongType(t *testing.T) {
	var sc ScrapersConfig
	err := json.Unmarshal([]byte(`{"scrape_actress": "not-a-bool"}`), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scrape_actress must be a boolean")
}

func TestScrapersConfigUnmarshalJSON_BrowserWrongType(t *testing.T) {
	var sc ScrapersConfig
	err := json.Unmarshal([]byte(`{"browser": "not-an-object"}`), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal browser")
}

func TestScrapersConfigUnmarshalJSON_UnknownScraperWithResolver(t *testing.T) {
	var sc ScrapersConfig
	sc.resolver = &staticTestConfigResolver{registered: map[string]bool{"r18dev": true}}
	err := json.Unmarshal([]byte(`{"mystery_scraper": {"enabled": true}}`), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown scraper")
}

func TestScrapersConfigUnmarshalJSON_NullAndEmptyObjectScraperSkipped(t *testing.T) {
	var sc ScrapersConfig
	require.NoError(t, json.Unmarshal([]byte(`{"r18dev": null, "dmm": {}}`), &sc))
	assert.Empty(t, sc.Overrides)
}

func TestScrapersConfigUnmarshalJSON_ScraperDecodeError(t *testing.T) {
	var sc ScrapersConfig
	// timeout must be an integer — strict decode rejects the wrong type.
	err := json.Unmarshal([]byte(`{"r18dev": {"timeout": "not-an-int"}}`), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode config for scraper")
}

func TestScrapersConfigUnmarshalJSON_AliasPathScraperRawDecodeError(t *testing.T) {
	var sc ScrapersConfig
	// rawVal contains the "request_delay" substring so the alias path is taken,
	// but the value is a JSON string (not an object) so decoding into the
	// map[string]json.RawMessage fails at the first alias-path decode.
	err := json.Unmarshal([]byte(`{"r18dev": "request_delay"}`), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode config for scraper")
}

func TestScrapersConfigUnmarshalJSON_AliasPathDecodeError(t *testing.T) {
	var sc ScrapersConfig
	// Presence of request_delay triggers the alias-decode path; an invalid
	// type on a real ScraperSettings field (rate_limit) makes the inner decode fail.
	err := json.Unmarshal([]byte(`{"r18dev": {"request_delay": 5, "rate_limit": "not-an-int"}}`), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode config for scraper")
}

func TestScrapersConfigUnmarshalJSON_AliasPathUnknownField(t *testing.T) {
	var sc ScrapersConfig
	// Alias path + an unknown field triggers the manual key validation error.
	err := json.Unmarshal([]byte(`{"r18dev": {"request_delay": 5, "bogus_field": 1}}`), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown field")
}
