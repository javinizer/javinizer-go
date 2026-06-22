package scraperconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveScraperProxyMode(t *testing.T) {
	global := ProxyConfig{
		Enabled:        true,
		DefaultProfile: "main",
		Profiles: map[string]ProxyProfile{
			"main":   {URL: "http://main-proxy.example.com:8080"},
			"backup": {URL: "http://backup-proxy.example.com:8080"},
		},
	}

	t.Run("global disabled returns direct", func(t *testing.T) {
		disabledGlobal := ProxyConfig{Enabled: false}
		mode := ResolveScraperProxyMode(disabledGlobal, &ProxyConfig{Enabled: true, Profile: "main"})
		assert.Equal(t, ScraperProxyModeDirect, mode)
	})

	t.Run("nil override returns inherit", func(t *testing.T) {
		mode := ResolveScraperProxyMode(global, nil)
		assert.Equal(t, ScraperProxyModeInherit, mode)
	})

	t.Run("disabled override returns direct", func(t *testing.T) {
		override := &ProxyConfig{Enabled: false}
		mode := ResolveScraperProxyMode(global, override)
		assert.Equal(t, ScraperProxyModeDirect, mode)
	})

	t.Run("enabled without profile returns inherit", func(t *testing.T) {
		override := &ProxyConfig{Enabled: true}
		mode := ResolveScraperProxyMode(global, override)
		assert.Equal(t, ScraperProxyModeInherit, mode)
	})

	t.Run("enabled with profile returns specific", func(t *testing.T) {
		override := &ProxyConfig{Enabled: true, Profile: "backup"}
		mode := ResolveScraperProxyMode(global, override)
		assert.Equal(t, ScraperProxyModeSpecific, mode)
	})
}

func TestResolveGlobalProxy(t *testing.T) {
	t.Run("disabled returns empty profile", func(t *testing.T) {
		global := ProxyConfig{Enabled: false}
		resolved := ResolveGlobalProxy(global)
		assert.Equal(t, &ProxyProfile{}, resolved)
	})

	t.Run("enabled with default profile returns that profile", func(t *testing.T) {
		global := ProxyConfig{
			Enabled:        true,
			DefaultProfile: "main",
			Profiles: map[string]ProxyProfile{
				"main": {
					URL:      "http://main-proxy.example.com:8080",
					Username: "main-user",
					Password: "main-pass",
				},
			},
		}
		resolved := ResolveGlobalProxy(global)
		assert.Equal(t, "http://main-proxy.example.com:8080", resolved.URL)
		assert.Equal(t, "main-user", resolved.Username)
		assert.Equal(t, "main-pass", resolved.Password)
	})

	t.Run("enabled with nonexistent default profile returns empty", func(t *testing.T) {
		global := ProxyConfig{
			Enabled:        true,
			DefaultProfile: "missing",
			Profiles:       map[string]ProxyProfile{},
		}
		resolved := ResolveGlobalProxy(global)
		assert.Equal(t, &ProxyProfile{}, resolved)
	})

	t.Run("enabled without default profile returns empty", func(t *testing.T) {
		global := ProxyConfig{
			Enabled:  true,
			Profiles: map[string]ProxyProfile{"main": {URL: "http://main.example.com:8080"}},
		}
		resolved := ResolveGlobalProxy(global)
		assert.Equal(t, &ProxyProfile{}, resolved)
	})
}

func TestResolveScraperProxy(t *testing.T) {
	global := ProxyConfig{
		Enabled:        true,
		DefaultProfile: "main",
		Profiles: map[string]ProxyProfile{
			"main": {
				URL:      "http://main-proxy.example.com:8080",
				Username: "main-user",
				Password: "main-pass",
			},
			"backup": {
				URL:      "http://backup-proxy.example.com:8080",
				Username: "backup-user",
				Password: "backup-pass",
			},
		},
	}

	t.Run("direct mode returns empty profile when global disabled", func(t *testing.T) {
		disabledGlobal := ProxyConfig{Enabled: false}
		override := &ProxyConfig{Enabled: true, Profile: "main"}
		profile := ResolveScraperProxy(disabledGlobal, override)
		assert.Equal(t, "", profile.URL)
	})

	t.Run("direct mode returns empty profile when override disabled", func(t *testing.T) {
		override := &ProxyConfig{Enabled: false}
		profile := ResolveScraperProxy(global, override)
		assert.Equal(t, "", profile.URL)
	})

	t.Run("inherit mode returns global default when nil override", func(t *testing.T) {
		profile := ResolveScraperProxy(global, nil)
		assert.Equal(t, "http://main-proxy.example.com:8080", profile.URL)
		assert.Equal(t, "main-user", profile.Username)
		assert.Equal(t, "main-pass", profile.Password)
	})

	t.Run("inherit mode returns global default when enabled without profile", func(t *testing.T) {
		override := &ProxyConfig{Enabled: true}
		profile := ResolveScraperProxy(global, override)
		assert.Equal(t, "http://main-proxy.example.com:8080", profile.URL)
	})

	t.Run("specific mode returns named profile", func(t *testing.T) {
		override := &ProxyConfig{Enabled: true, Profile: "backup"}
		profile := ResolveScraperProxy(global, override)
		assert.Equal(t, "http://backup-proxy.example.com:8080", profile.URL)
		assert.Equal(t, "backup-user", profile.Username)
		assert.Equal(t, "backup-pass", profile.Password)
	})

	t.Run("specific mode inherits credentials from global when omitted", func(t *testing.T) {
		globalWithCreds := ProxyConfig{
			Enabled:        true,
			DefaultProfile: "main",
			Profiles: map[string]ProxyProfile{
				"main": {
					URL:      "http://main-proxy.example.com:8080",
					Username: "global-user",
					Password: "global-pass",
				},
				"specific": {
					URL: "http://specific-proxy.example.com:9090",
				},
			},
		}
		override := &ProxyConfig{Enabled: true, Profile: "specific"}
		profile := ResolveScraperProxy(globalWithCreds, override)
		assert.Equal(t, "http://specific-proxy.example.com:9090", profile.URL)
		assert.Equal(t, "global-user", profile.Username)
		assert.Equal(t, "global-pass", profile.Password)
	})

	t.Run("specific mode with nonexistent profile falls back to inherit", func(t *testing.T) {
		override := &ProxyConfig{Enabled: true, Profile: "nonexistent"}
		profile := ResolveScraperProxy(global, override)
		assert.Equal(t, "http://main-proxy.example.com:8080", profile.URL)
		assert.Equal(t, "main-user", profile.Username)
		assert.Equal(t, "main-pass", profile.Password)
	})
}
