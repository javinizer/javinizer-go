package scraperconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScraperSettings_Clone_ZeroValue(t *testing.T) {
	t.Parallel()

	var s ScraperSettings
	clone := s.Clone()
	assert.Equal(t, s, clone)
}

func TestScraperSettings_Clone_BasicFields(t *testing.T) {
	t.Parallel()

	s := ScraperSettings{
		Enabled:         true,
		Language:        "ja",
		Timeout:         30,
		RateLimit:       1000,
		RetryCount:      3,
		UserAgent:       "test-agent",
		UseFlareSolverr: true,
		UseBrowser:      true,
		BaseURL:         "https://example.com",
	}

	clone := s.Clone()
	assert.Equal(t, s.Enabled, clone.Enabled)
	assert.Equal(t, s.Language, clone.Language)
	assert.Equal(t, s.Timeout, clone.Timeout)
	assert.Equal(t, s.RateLimit, clone.RateLimit)
	assert.Equal(t, s.RetryCount, clone.RetryCount)
	assert.Equal(t, s.UserAgent, clone.UserAgent)
	assert.Equal(t, s.UseFlareSolverr, clone.UseFlareSolverr)
	assert.Equal(t, s.UseBrowser, clone.UseBrowser)
	assert.Equal(t, s.BaseURL, clone.BaseURL)

	clone.Enabled = false
	assert.True(t, s.Enabled, "mutating clone should not affect original")
}

func TestScraperSettings_Clone_Proxy(t *testing.T) {
	t.Parallel()

	s := ScraperSettings{
		Proxy: &ProxyConfig{
			Enabled:        true,
			Profile:        "test-profile",
			DefaultProfile: "default",
			Profiles: map[string]ProxyProfile{
				"p1": {URL: "http://proxy1:8080", Username: "user1", Password: "pass1"},
			},
		},
	}

	clone := s.Clone()
	assert.NotNil(t, clone.Proxy)
	assert.Equal(t, s.Proxy.Enabled, clone.Proxy.Enabled)
	assert.Equal(t, s.Proxy.Profile, clone.Proxy.Profile)
	assert.Equal(t, s.Proxy.DefaultProfile, clone.Proxy.DefaultProfile)

	clone.Proxy.Profile = "modified"
	assert.Equal(t, "test-profile", s.Proxy.Profile, "mutating clone proxy should not affect original")

	clone.Proxy.Profiles["p1"] = ProxyProfile{URL: "http://modified:9999"}
	assert.Equal(t, "http://proxy1:8080", s.Proxy.Profiles["p1"].URL, "mutating clone proxy profiles should not affect original")

	delete(clone.Proxy.Profiles, "p1")
	_, exists := s.Proxy.Profiles["p1"]
	assert.True(t, exists, "deleting from clone profiles should not affect original")
}

func TestScraperSettings_Clone_DownloadProxy(t *testing.T) {
	t.Parallel()

	s := ScraperSettings{
		DownloadProxy: &ProxyConfig{
			Enabled: true,
			Profile: "dl-profile",
			Profiles: map[string]ProxyProfile{
				"dl1": {URL: "http://dlproxy:8080"},
			},
		},
	}

	clone := s.Clone()
	assert.NotNil(t, clone.DownloadProxy)

	clone.DownloadProxy.Profile = "modified"
	assert.Equal(t, "dl-profile", s.DownloadProxy.Profile, "mutating clone download proxy should not affect original")
}

func TestScraperSettings_Clone_NilProxy(t *testing.T) {
	t.Parallel()

	s := ScraperSettings{Proxy: nil, DownloadProxy: nil}
	clone := s.Clone()
	assert.Nil(t, clone.Proxy)
	assert.Nil(t, clone.DownloadProxy)
}

func TestScraperSettings_Clone_ScrapeActress(t *testing.T) {
	t.Parallel()

	t.Run("nil", func(t *testing.T) {
		t.Parallel()
		s := ScraperSettings{ScrapeActress: nil}
		clone := s.Clone()
		assert.Nil(t, clone.ScrapeActress)
	})

	t.Run("pointer_isolation", func(t *testing.T) {
		t.Parallel()
		val := true
		s := ScraperSettings{ScrapeActress: &val}
		clone := s.Clone()

		assert.NotNil(t, clone.ScrapeActress)
		assert.Equal(t, *s.ScrapeActress, *clone.ScrapeActress)

		*clone.ScrapeActress = false
		assert.True(t, *s.ScrapeActress, "mutating clone ScrapeActress should not affect original")
	})
}

func TestScraperSettings_Clone_Cookies(t *testing.T) {
	t.Parallel()

	s := ScraperSettings{
		Cookies: map[string]string{
			"session": "abc123",
			"token":   "xyz789",
		},
	}

	clone := s.Clone()
	assert.Equal(t, len(s.Cookies), len(clone.Cookies))
	assert.Equal(t, s.Cookies["session"], clone.Cookies["session"])

	clone.Cookies["session"] = "modified"
	assert.Equal(t, "abc123", s.Cookies["session"], "mutating clone cookies should not affect original")

	delete(clone.Cookies, "token")
	_, exists := s.Cookies["token"]
	assert.True(t, exists, "deleting from clone cookies should not affect original")
}

func TestScraperSettings_Clone_NilCookies(t *testing.T) {
	t.Parallel()

	s := ScraperSettings{Cookies: nil}
	clone := s.Clone()
	assert.Nil(t, clone.Cookies)
}

func TestScraperSettings_Clone_ExtraPlaceholderHashes(t *testing.T) {
	t.Parallel()

	t.Run("nil", func(t *testing.T) {
		t.Parallel()
		s := ScraperSettings{ExtraPlaceholderHashes: nil}
		clone := s.Clone()
		assert.Nil(t, clone.ExtraPlaceholderHashes)
	})

	t.Run("slice_isolation", func(t *testing.T) {
		t.Parallel()
		s := ScraperSettings{
			ExtraPlaceholderHashes: []string{"hash1", "hash2", "hash3"},
		}
		clone := s.Clone()
		assert.Equal(t, s.ExtraPlaceholderHashes, clone.ExtraPlaceholderHashes)

		clone.ExtraPlaceholderHashes[0] = "modified"
		assert.Equal(t, "hash1", s.ExtraPlaceholderHashes[0], "mutating clone slice should not affect original")
	})
}
