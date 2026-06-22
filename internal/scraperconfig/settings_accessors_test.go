package scraperconfig

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestScraperSettings_ShouldScrapeActress(t *testing.T) {
	t.Parallel()

	t.Run("nil receiver returns global default", func(t *testing.T) {
		var s *ScraperSettings
		assert.True(t, s.ShouldScrapeActress(true))
		assert.False(t, s.ShouldScrapeActress(false))
	})

	t.Run("nil ScrapeActress field falls back to global", func(t *testing.T) {
		s := &ScraperSettings{ScrapeActress: nil}
		assert.True(t, s.ShouldScrapeActress(true))
		assert.False(t, s.ShouldScrapeActress(false))
	})

	t.Run("non-nil ScrapeActress overrides global", func(t *testing.T) {
		truth := true
		falsch := false
		s1 := &ScraperSettings{ScrapeActress: &truth}
		assert.True(t, s1.ShouldScrapeActress(false))

		s2 := &ScraperSettings{ScrapeActress: &falsch}
		assert.False(t, s2.ShouldScrapeActress(true))
	})
}

func TestScraperSettings_ShouldRespectRetryAfter(t *testing.T) {
	t.Parallel()

	t.Run("nil receiver returns global default", func(t *testing.T) {
		var s *ScraperSettings
		assert.True(t, s.ShouldRespectRetryAfter(true))
		assert.False(t, s.ShouldRespectRetryAfter(false))
	})

	t.Run("nil RespectRetryAfter field falls back to global", func(t *testing.T) {
		s := &ScraperSettings{RespectRetryAfter: nil}
		assert.True(t, s.ShouldRespectRetryAfter(true))
		assert.False(t, s.ShouldRespectRetryAfter(false))
	})

	t.Run("non-nil RespectRetryAfter overrides global", func(t *testing.T) {
		truth := true
		falsch := false
		s1 := &ScraperSettings{RespectRetryAfter: &truth}
		assert.True(t, s1.ShouldRespectRetryAfter(false))

		s2 := &ScraperSettings{RespectRetryAfter: &falsch}
		assert.False(t, s2.ShouldRespectRetryAfter(true))
	})
}

func TestScraperSettings_ShouldUseBrowser(t *testing.T) {
	t.Parallel()

	t.Run("nil receiver returns false", func(t *testing.T) {
		var s *ScraperSettings
		assert.False(t, s.ShouldUseBrowser(true))
		assert.False(t, s.ShouldUseBrowser(false))
	})
	t.Run("global disabled returns false", func(t *testing.T) {
		s := &ScraperSettings{UseBrowser: true}
		assert.False(t, s.ShouldUseBrowser(false))
	})

	t.Run("global enabled and scraper enabled returns true", func(t *testing.T) {
		s := &ScraperSettings{UseBrowser: true}
		assert.True(t, s.ShouldUseBrowser(true))
	})

	t.Run("global enabled but scraper disabled returns false", func(t *testing.T) {
		s := &ScraperSettings{UseBrowser: false}
		assert.False(t, s.ShouldUseBrowser(true))
	})
}

func TestScraperSettings_MarshalYAML(t *testing.T) {
	t.Parallel()

	t.Run("nil receiver returns nil", func(t *testing.T) {
		var s *ScraperSettings
		got, err := s.MarshalYAML()
		assert.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("basic fields", func(t *testing.T) {
		s := &ScraperSettings{
			Enabled:    true,
			Language:   "ja",
			Timeout:    30,
			RateLimit:  1000,
			RetryCount: 3,
			UserAgent:  "test-agent",
		}
		got, err := s.MarshalYAML()
		require.NoError(t, err)
		m, ok := got.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, m["enabled"])
		assert.Equal(t, "ja", m["language"])
		assert.Equal(t, 30, m["timeout"])
	})

	t.Run("optional fields omitted when zero", func(t *testing.T) {
		s := &ScraperSettings{Enabled: true}
		got, err := s.MarshalYAML()
		require.NoError(t, err)
		m, ok := got.(map[string]any)
		require.True(t, ok)
		_, hasProxy := m["proxy"]
		assert.False(t, hasProxy, "proxy should be omitted when nil")
		_, hasBaseURL := m["base_url"]
		assert.False(t, hasBaseURL, "base_url should be omitted when empty")
		_, hasAPIKey := m["api_key"]
		assert.False(t, hasAPIKey, "api_key should be omitted when empty")
	})

	t.Run("optional fields present when set", func(t *testing.T) {
		s := &ScraperSettings{
			Enabled:                true,
			Proxy:                  &ProxyConfig{Enabled: true, Profile: "main"},
			BaseURL:                "https://example.com",
			APIKey:                 "secret",
			Cookies:                map[string]string{"session": "abc"},
			PlaceholderThresholdKB: 50,
			ExtraPlaceholderHashes: []string{"hash1"},
			ScrapeBonusScreens:     true,
		}
		got, err := s.MarshalYAML()
		require.NoError(t, err)
		m, ok := got.(map[string]any)
		require.True(t, ok)
		assert.NotNil(t, m["proxy"])
		assert.Equal(t, "https://example.com", m["base_url"])
		assert.Equal(t, "secret", m["api_key"])
		assert.NotNil(t, m["cookies"])
		assert.Equal(t, 50, m["placeholder_threshold"])
		assert.NotNil(t, m["extra_placeholder_hashes"])
		assert.Equal(t, true, m["scrape_bonus_screens"])
	})

	t.Run("ScrapeActress pointer", func(t *testing.T) {
		val := true
		s := &ScraperSettings{ScrapeActress: &val}
		got, err := s.MarshalYAML()
		require.NoError(t, err)
		m, ok := got.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, m["scrape_actress"])
	})
}

func TestScraperSettings_MarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("round-trip", func(t *testing.T) {
		s := &ScraperSettings{
			Enabled:    true,
			Language:   "en",
			Timeout:    60,
			RateLimit:  500,
			RetryCount: 3,
			UserAgent:  "test",
			BaseURL:    "https://example.com",
		}
		data, err := json.Marshal(s)
		require.NoError(t, err)

		// Verify the JSON can be unmarshaled back
		var raw map[string]any
		require.NoError(t, json.Unmarshal(data, &raw))
		assert.Equal(t, true, raw["enabled"])
		assert.Equal(t, "en", raw["language"])
		assert.Equal(t, "https://example.com", raw["base_url"])
	})

	t.Run("nil settings MarshalJSON error", func(t *testing.T) {
		var s *ScraperSettings
		// MarshalJSON on nil should handle gracefully via MarshalYAML returning nil
		_, err := json.Marshal(s)
		// json.Marshal of nil pointer to struct produces null
		assert.NoError(t, err)
	})
}

func TestProxyConfig_UnmarshalYAML(t *testing.T) {
	t.Parallel()

	t.Run("valid proxy config", func(t *testing.T) {
		var p ProxyConfig
		err := yaml.Unmarshal([]byte("enabled: true\nprofile: main\n"), &p)
		require.NoError(t, err)
		assert.True(t, p.Enabled)
		assert.Equal(t, "main", p.Profile)
	})

	t.Run("rejects legacy url field", func(t *testing.T) {
		var p ProxyConfig
		err := yaml.Unmarshal([]byte("url: http://proxy:8080\n"), &p)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no longer supported")
	})

	t.Run("rejects legacy username field", func(t *testing.T) {
		var p ProxyConfig
		err := yaml.Unmarshal([]byte("username: user\n"), &p)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no longer supported")
	})

	t.Run("rejects legacy password field", func(t *testing.T) {
		var p ProxyConfig
		err := yaml.Unmarshal([]byte("password: secret\n"), &p)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no longer supported")
	})

	t.Run("rejects legacy use_main_proxy field", func(t *testing.T) {
		var p ProxyConfig
		err := yaml.Unmarshal([]byte("use_main_proxy: true\n"), &p)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no longer supported")
	})

	t.Run("with profiles", func(t *testing.T) {
		input := `enabled: true
default_profile: main
profiles:
  main:
    url: http://proxy:8080
    username: user
    password: pass
`
		var p ProxyConfig
		err := yaml.Unmarshal([]byte(input), &p)
		require.NoError(t, err)
		assert.True(t, p.Enabled)
		assert.Equal(t, "main", p.DefaultProfile)
		assert.Equal(t, "http://proxy:8080", p.Profiles["main"].URL)
	})
}

func TestProxyConfig_UnmarshalYAML_NilNode(t *testing.T) {
	t.Parallel()
	// When YAML value is null, node is nil - should not panic
	var p ProxyConfig
	err := yaml.Unmarshal([]byte("proxy:\n"), &p)
	require.NoError(t, err)
}
