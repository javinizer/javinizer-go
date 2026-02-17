package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveScraperProxy(t *testing.T) {
	global := ProxyConfig{
		Enabled:  true,
		URL:      "http://global-proxy.example.com:8080",
		Username: "global-user",
		Password: "global-pass",
		FlareSolverr: FlareSolverrConfig{
			Enabled:    true,
			URL:        "http://global-fs:8191/v1",
			Timeout:    30,
			MaxRetries: 3,
			SessionTTL: 300,
		},
	}

	t.Run("uses global when override is nil", func(t *testing.T) {
		resolved := ResolveScraperProxy(global, nil)
		assert.Equal(t, global, *resolved)
	})

	t.Run("uses scraper override when provided", func(t *testing.T) {
		override := &ProxyConfig{
			Enabled: true,
			URL:     "http://scraper-proxy.example.com:8080",
			FlareSolverr: FlareSolverrConfig{
				Enabled:    true,
				URL:        "http://scraper-fs:8191/v1",
				Timeout:    45,
				MaxRetries: 2,
				SessionTTL: 600,
			},
		}

		resolved := ResolveScraperProxy(global, override)
		assert.Equal(t, *override, *resolved)
	})

	t.Run("reuses global proxy when use_main_proxy is set", func(t *testing.T) {
		override := &ProxyConfig{
			Enabled:      true,
			UseMainProxy: true,
		}

		resolved := ResolveScraperProxy(global, override)
		assert.Equal(t, global, *resolved)
	})

	t.Run("inherits global flaresolverr when scraper override omits it", func(t *testing.T) {
		override := &ProxyConfig{
			Enabled:  true,
			URL:      "http://scraper-proxy.example.com:8080",
			Username: "scraper-user",
			Password: "scraper-pass",
			// FlareSolverr block intentionally omitted (zero-value)
		}

		resolved := ResolveScraperProxy(global, override)
		assert.Equal(t, override.Enabled, resolved.Enabled)
		assert.Equal(t, override.URL, resolved.URL)
		assert.Equal(t, override.Username, resolved.Username)
		assert.Equal(t, override.Password, resolved.Password)
		assert.Equal(t, global.FlareSolverr, resolved.FlareSolverr)
	})

	t.Run("reuses global proxy but allows flaresolverr override", func(t *testing.T) {
		override := &ProxyConfig{
			Enabled:      true,
			UseMainProxy: true,
			FlareSolverr: FlareSolverrConfig{
				Enabled:    true,
				URL:        "http://scraper-fs:8191/v1",
				Timeout:    40,
				MaxRetries: 2,
				SessionTTL: 500,
			},
		}

		resolved := ResolveScraperProxy(global, override)
		assert.Equal(t, global.Enabled, resolved.Enabled)
		assert.Equal(t, global.URL, resolved.URL)
		assert.Equal(t, global.Username, resolved.Username)
		assert.Equal(t, global.Password, resolved.Password)
		assert.Equal(t, override.FlareSolverr, resolved.FlareSolverr)
	})
}
