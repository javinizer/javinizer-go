package scraperconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScraperSettings_MergeDefaultsFrom_StringFields(t *testing.T) {
	t.Parallel()

	defaults := ScraperSettings{
		Language:  "en",
		UserAgent: "Javinizer/1.0",
		BaseURL:   "https://example.com",
		APIKey:    "key123",
	}

	t.Run("empty_override_gets_default", func(t *testing.T) {
		t.Parallel()
		s := ScraperSettings{}
		s.MergeDefaultsFrom(defaults)
		assert.Equal(t, "en", s.Language)
		assert.Equal(t, "Javinizer/1.0", s.UserAgent)
		assert.Equal(t, "https://example.com", s.BaseURL)
		assert.Equal(t, "key123", s.APIKey)
	})

	t.Run("non_empty_override_wins", func(t *testing.T) {
		t.Parallel()
		s := ScraperSettings{
			Language:  "ja",
			UserAgent: "CustomAgent",
			BaseURL:   "https://custom.com",
			APIKey:    "custom-key",
		}
		s.MergeDefaultsFrom(defaults)
		assert.Equal(t, "ja", s.Language)
		assert.Equal(t, "CustomAgent", s.UserAgent)
		assert.Equal(t, "https://custom.com", s.BaseURL)
		assert.Equal(t, "custom-key", s.APIKey)
	})

	t.Run("partial_override_merges_only_empty", func(t *testing.T) {
		t.Parallel()
		s := ScraperSettings{UserAgent: "CustomAgent"}
		s.MergeDefaultsFrom(defaults)
		assert.Equal(t, "en", s.Language, "empty field should get default")
		assert.Equal(t, "CustomAgent", s.UserAgent, "non-empty field should keep override")
		assert.Equal(t, "https://example.com", s.BaseURL, "empty field should get default")
	})
}

func TestScraperSettings_MergeDefaultsFrom_IntFields(t *testing.T) {
	t.Parallel()

	defaults := ScraperSettings{
		RateLimit:              1000,
		Timeout:                30,
		RetryCount:             3,
		PlaceholderThresholdKB: 10,
	}

	t.Run("zero_override_gets_default", func(t *testing.T) {
		t.Parallel()
		s := ScraperSettings{}
		s.MergeDefaultsFrom(defaults)
		assert.Equal(t, 1000, s.RateLimit)
		assert.Equal(t, 30, s.Timeout)
		assert.Equal(t, 3, s.RetryCount)
		assert.Equal(t, 10, s.PlaceholderThresholdKB)
	})

	t.Run("non_zero_override_wins", func(t *testing.T) {
		t.Parallel()
		s := ScraperSettings{
			RateLimit:              500,
			Timeout:                60,
			RetryCount:             5,
			PlaceholderThresholdKB: 20,
		}
		s.MergeDefaultsFrom(defaults)
		assert.Equal(t, 500, s.RateLimit)
		assert.Equal(t, 60, s.Timeout)
		assert.Equal(t, 5, s.RetryCount)
		assert.Equal(t, 20, s.PlaceholderThresholdKB)
	})
}

func TestScraperSettings_MergeDefaultsFrom_PointerFields(t *testing.T) {
	t.Parallel()

	t.Run("ScrapeActress_nil_override_gets_default", func(t *testing.T) {
		t.Parallel()
		val := true
		defaults := ScraperSettings{ScrapeActress: &val}
		s := ScraperSettings{}
		s.MergeDefaultsFrom(defaults)
		assert.NotNil(t, s.ScrapeActress)
		assert.True(t, *s.ScrapeActress)
	})

	t.Run("ScrapeActress_non_nil_override_wins", func(t *testing.T) {
		t.Parallel()
		defaultVal := true
		overrideVal := false
		defaults := ScraperSettings{ScrapeActress: &defaultVal}
		s := ScraperSettings{ScrapeActress: &overrideVal}
		s.MergeDefaultsFrom(defaults)
		assert.False(t, *s.ScrapeActress, "non-nil override should win")
	})

	t.Run("ScrapeActress_pointer_isolation", func(t *testing.T) {
		t.Parallel()
		val := true
		defaults := ScraperSettings{ScrapeActress: &val}
		s := ScraperSettings{}
		s.MergeDefaultsFrom(defaults)
		*s.ScrapeActress = false
		assert.True(t, val, "mutating merged pointer should not affect defaults")
	})

	t.Run("RespectRetryAfter_nil_override_gets_default", func(t *testing.T) {
		t.Parallel()
		val := true
		defaults := ScraperSettings{RespectRetryAfter: &val}
		s := ScraperSettings{}
		s.MergeDefaultsFrom(defaults)
		assert.NotNil(t, s.RespectRetryAfter)
		assert.True(t, *s.RespectRetryAfter)
	})

	t.Run("RespectRetryAfter_non_nil_override_wins", func(t *testing.T) {
		t.Parallel()
		defaultVal := true
		overrideVal := false
		defaults := ScraperSettings{RespectRetryAfter: &defaultVal}
		s := ScraperSettings{RespectRetryAfter: &overrideVal}
		s.MergeDefaultsFrom(defaults)
		assert.False(t, *s.RespectRetryAfter)
	})

	t.Run("RespectRetryAfter_pointer_isolation", func(t *testing.T) {
		t.Parallel()
		val := true
		defaults := ScraperSettings{RespectRetryAfter: &val}
		s := ScraperSettings{}
		s.MergeDefaultsFrom(defaults)
		*s.RespectRetryAfter = false
		assert.True(t, val, "mutating merged pointer should not affect defaults")
	})
}

func TestScraperSettings_MergeDefaultsFrom_ExcludedFields(t *testing.T) {
	t.Parallel()

	t.Run("Enabled_is_never_merged", func(t *testing.T) {
		t.Parallel()
		defaults := ScraperSettings{Enabled: true}
		s := ScraperSettings{Enabled: false}
		s.MergeDefaultsFrom(defaults)
		assert.False(t, s.Enabled, "Enabled should not be merged from defaults")
	})

	t.Run("Proxy_is_never_merged", func(t *testing.T) {
		t.Parallel()
		proxyVal := ProxyConfig{Enabled: true, Profile: "my-profile"}
		defaults := ScraperSettings{Proxy: &proxyVal}
		s := ScraperSettings{Proxy: nil}
		s.MergeDefaultsFrom(defaults)
		assert.Nil(t, s.Proxy, "Proxy should not be merged from defaults")
	})

	t.Run("DownloadProxy_is_never_merged", func(t *testing.T) {
		t.Parallel()
		proxyVal := ProxyConfig{Enabled: true, Profile: "dl-profile"}
		defaults := ScraperSettings{DownloadProxy: &proxyVal}
		s := ScraperSettings{DownloadProxy: nil}
		s.MergeDefaultsFrom(defaults)
		assert.Nil(t, s.DownloadProxy, "DownloadProxy should not be merged from defaults")
	})

	t.Run("UseFlareSolverr_is_never_merged", func(t *testing.T) {
		t.Parallel()
		defaults := ScraperSettings{UseFlareSolverr: true}
		s := ScraperSettings{UseFlareSolverr: false}
		s.MergeDefaultsFrom(defaults)
		assert.False(t, s.UseFlareSolverr, "UseFlareSolverr should not be merged")
	})

	t.Run("UseBrowser_is_never_merged", func(t *testing.T) {
		t.Parallel()
		defaults := ScraperSettings{UseBrowser: true}
		s := ScraperSettings{UseBrowser: false}
		s.MergeDefaultsFrom(defaults)
		assert.False(t, s.UseBrowser, "UseBrowser should not be merged")
	})

	t.Run("ScrapeBonusScreens_is_never_merged", func(t *testing.T) {
		t.Parallel()
		defaults := ScraperSettings{ScrapeBonusScreens: true}
		s := ScraperSettings{ScrapeBonusScreens: false}
		s.MergeDefaultsFrom(defaults)
		assert.False(t, s.ScrapeBonusScreens, "ScrapeBonusScreens should not be merged")
	})

	t.Run("Cookies_is_never_merged", func(t *testing.T) {
		t.Parallel()
		defaults := ScraperSettings{Cookies: map[string]string{"key": "val"}}
		s := ScraperSettings{Cookies: nil}
		s.MergeDefaultsFrom(defaults)
		assert.Nil(t, s.Cookies, "Cookies should not be merged")
	})

	t.Run("ExtraPlaceholderHashes_is_never_merged", func(t *testing.T) {
		t.Parallel()
		defaults := ScraperSettings{ExtraPlaceholderHashes: []string{"hash1"}}
		s := ScraperSettings{ExtraPlaceholderHashes: nil}
		s.MergeDefaultsFrom(defaults)
		assert.Nil(t, s.ExtraPlaceholderHashes, "ExtraPlaceholderHashes should not be merged")
	})
}

func TestScraperSettings_MergeDefaultsFrom_BothZero(t *testing.T) {
	t.Parallel()

	// When both defaults and override have zero values, nothing changes
	defaults := ScraperSettings{Language: ""}
	s := ScraperSettings{Language: ""}
	s.MergeDefaultsFrom(defaults)
	assert.Equal(t, "", s.Language, "both zero should remain zero")
}
