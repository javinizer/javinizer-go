package system

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestPreserveRedactedSecrets_OverrideLoop_PatchGaps(t *testing.T) {
	t.Run("nil old override entry is skipped leaving new redacted values untouched", func(t *testing.T) {
		old := config.DefaultConfig(nil, nil)
		old.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javstash": nil,
		}

		newCfg := config.DefaultConfig(nil, nil)
		newCfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javstash": {
				APIKey: models.RedactedValue,
				Proxy: &models.ProxyConfig{
					Profiles: map[string]models.ProxyProfile{
						"main": {Username: models.RedactedValue, Password: models.RedactedValue},
					},
				},
				DownloadProxy: &models.ProxyConfig{
					Profiles: map[string]models.ProxyProfile{
						"main": {Username: models.RedactedValue, Password: models.RedactedValue},
					},
				},
			},
		}

		preserveRedactedSecrets(old, newCfg)

		override := newCfg.Scrapers.Overrides["javstash"]
		assert.Equal(t, models.RedactedValue, override.APIKey)
		assert.Equal(t, models.RedactedValue, override.Proxy.Profiles["main"].Username)
		assert.Equal(t, models.RedactedValue, override.Proxy.Profiles["main"].Password)
		assert.Equal(t, models.RedactedValue, override.DownloadProxy.Profiles["main"].Username)
		assert.Equal(t, models.RedactedValue, override.DownloadProxy.Profiles["main"].Password)
	})

	t.Run("redacted scraper override proxy profile credentials are restored from old", func(t *testing.T) {
		old := config.DefaultConfig(nil, nil)
		old.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javstash": {
				Proxy: &models.ProxyConfig{
					Profiles: map[string]models.ProxyProfile{
						"main": {URL: "http://old-proxy:8080", Username: "old-user", Password: "old-pass"},
					},
				},
			},
		}

		newCfg := config.DefaultConfig(nil, nil)
		newCfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javstash": {
				Proxy: &models.ProxyConfig{
					Profiles: map[string]models.ProxyProfile{
						"main": {URL: "http://new-proxy:8080", Username: models.RedactedValue, Password: models.RedactedValue},
					},
				},
			},
		}

		preserveRedactedSecrets(old, newCfg)

		profile := newCfg.Scrapers.Overrides["javstash"].Proxy.Profiles["main"]
		assert.Equal(t, "http://new-proxy:8080", profile.URL)
		assert.Equal(t, "old-user", profile.Username)
		assert.Equal(t, "old-pass", profile.Password)
	})

	t.Run("redacted scraper override download proxy profile credentials are restored from old", func(t *testing.T) {
		old := config.DefaultConfig(nil, nil)
		old.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javstash": {
				DownloadProxy: &models.ProxyConfig{
					Profiles: map[string]models.ProxyProfile{
						"dl": {URL: "http://old-dl:9090", Username: "dl-user", Password: "dl-pass"},
					},
				},
			},
		}

		newCfg := config.DefaultConfig(nil, nil)
		newCfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javstash": {
				DownloadProxy: &models.ProxyConfig{
					Profiles: map[string]models.ProxyProfile{
						"dl": {URL: "http://new-dl:9090", Username: models.RedactedValue, Password: models.RedactedValue},
					},
				},
			},
		}

		preserveRedactedSecrets(old, newCfg)

		profile := newCfg.Scrapers.Overrides["javstash"].DownloadProxy.Profiles["dl"]
		assert.Equal(t, "http://new-dl:9090", profile.URL)
		assert.Equal(t, "dl-user", profile.Username)
		assert.Equal(t, "dl-pass", profile.Password)
	})

	t.Run("only one side carrying a ProxyConfig skips proxy profile preservation", func(t *testing.T) {
		old := config.DefaultConfig(nil, nil)
		old.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javstash": {},
		}
		newCfg := config.DefaultConfig(nil, nil)
		newCfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javstash": {
				Proxy: &models.ProxyConfig{
					Profiles: map[string]models.ProxyProfile{
						"main": {Username: models.RedactedValue, Password: models.RedactedValue},
					},
				},
			},
		}

		preserveRedactedSecrets(old, newCfg)

		profile := newCfg.Scrapers.Overrides["javstash"].Proxy.Profiles["main"]
		assert.Equal(t, models.RedactedValue, profile.Username)
		assert.Equal(t, models.RedactedValue, profile.Password)
	})

	t.Run("only one side carrying a DownloadProxy skips download profile preservation", func(t *testing.T) {
		old := config.DefaultConfig(nil, nil)
		old.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javstash": {},
		}
		newCfg := config.DefaultConfig(nil, nil)
		newCfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javstash": {
				DownloadProxy: &models.ProxyConfig{
					Profiles: map[string]models.ProxyProfile{
						"dl": {Username: models.RedactedValue, Password: models.RedactedValue},
					},
				},
			},
		}

		preserveRedactedSecrets(old, newCfg)

		profile := newCfg.Scrapers.Overrides["javstash"].DownloadProxy.Profiles["dl"]
		assert.Equal(t, models.RedactedValue, profile.Username)
		assert.Equal(t, models.RedactedValue, profile.Password)
	})

	t.Run("full override block restores proxy, download proxy, and APIKey together", func(t *testing.T) {
		old := config.DefaultConfig(nil, nil)
		old.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javstash": {
				APIKey: "real-api-key",
				Proxy: &models.ProxyConfig{
					Profiles: map[string]models.ProxyProfile{
						"main": {URL: "http://old:8080", Username: "u", Password: "p"},
					},
				},
				DownloadProxy: &models.ProxyConfig{
					Profiles: map[string]models.ProxyProfile{
						"dl": {URL: "http://old-dl:9090", Username: "du", Password: "dp"},
					},
				},
			},
		}

		newCfg := config.DefaultConfig(nil, nil)
		newCfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javstash": {
				APIKey: models.RedactedValue,
				Proxy: &models.ProxyConfig{
					Profiles: map[string]models.ProxyProfile{
						"main": {URL: "http://new:8080", Username: models.RedactedValue, Password: models.RedactedValue},
					},
				},
				DownloadProxy: &models.ProxyConfig{
					Profiles: map[string]models.ProxyProfile{
						"dl": {URL: "http://new-dl:9090", Username: models.RedactedValue, Password: models.RedactedValue},
					},
				},
			},
		}

		preserveRedactedSecrets(old, newCfg)

		override := newCfg.Scrapers.Overrides["javstash"]
		assert.Equal(t, "real-api-key", override.APIKey)
		assert.Equal(t, "u", override.Proxy.Profiles["main"].Username)
		assert.Equal(t, "p", override.Proxy.Profiles["main"].Password)
		assert.Equal(t, "http://new:8080", override.Proxy.Profiles["main"].URL)
		assert.Equal(t, "du", override.DownloadProxy.Profiles["dl"].Username)
		assert.Equal(t, "dp", override.DownloadProxy.Profiles["dl"].Password)
		assert.Equal(t, "http://new-dl:9090", override.DownloadProxy.Profiles["dl"].URL)
	})

	t.Run("override present in old but absent from new is not created", func(t *testing.T) {
		old := config.DefaultConfig(nil, nil)
		old.Scrapers.Overrides = map[string]*models.ScraperSettings{
			"javstash": {APIKey: "real-api-key"},
		}
		newCfg := config.DefaultConfig(nil, nil)
		newCfg.Scrapers.Overrides = map[string]*models.ScraperSettings{}

		preserveRedactedSecrets(old, newCfg)

		_, ok := newCfg.Scrapers.UserOverride("javstash")
		assert.False(t, ok)
	})
}
