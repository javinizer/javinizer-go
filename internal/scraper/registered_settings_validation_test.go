package scraper_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisteredScraperSettingsPublicValidation(t *testing.T) {
	for _, module := range []struct {
		name            string
		defaultURL      string
		defaultLanguage string
		languages       []string
		languageError   string
	}{
		{
			name: "caribbeancom", defaultURL: "https://www.caribbeancom.com", defaultLanguage: "ja",
			languages: []string{"ja", "en"}, languageError: "language must be 'ja' or 'en'",
		},
		{
			name: "aventertainment", defaultURL: "https://www.aventertainments.com", defaultLanguage: "en",
			languages: []string{"en", "ja"}, languageError: "language must be 'en' or 'ja'",
		},
		{
			name: "tokyohot", defaultURL: "https://www.tokyo-hot.com", defaultLanguage: "ja",
			languages: []string{"en", "ja", "zh"}, languageError: "language must be 'en', 'ja', or 'zh'",
		},
		{name: "dlgetchu", defaultURL: "http://dl.getchu.com"},
		{name: "jav321", defaultURL: "https://jp.jav321.com", defaultLanguage: "ja"},
		{name: "libredmm", defaultURL: "https://www.libredmm.com"},
	} {
		t.Run(module.name, func(t *testing.T) {
			type validationCase struct {
				name     string
				settings models.ScraperSettings
				wantErr  string
			}
			cases := []validationCase{
				{name: "enabled with registered defaults", settings: models.ScraperSettings{Enabled: true}},
				{name: "disabled by default"},
				{name: "http mirror", settings: models.ScraperSettings{Enabled: true, BaseURL: "http://mirror.example.invalid/catalog"}},
				{name: "https mirror", settings: models.ScraperSettings{Enabled: true, BaseURL: "https://mirror.example.invalid/catalog"}},
				{name: "padded base URL", settings: models.ScraperSettings{Enabled: true, BaseURL: " \thttps://mirror.example.invalid/ \n"}},
				{
					name: "unsupported URL scheme", settings: models.ScraperSettings{Enabled: true, BaseURL: "file:///fixture"},
					wantErr: module.name + ".base_url must be a valid HTTP or HTTPS URL",
				},
				{
					name: "malformed URL", settings: models.ScraperSettings{Enabled: true, BaseURL: "https://[broken"},
					wantErr: module.name + ".base_url must be a valid URL:",
				},
				{
					name: "missing URL host", settings: models.ScraperSettings{Enabled: true, BaseURL: "https:///catalog"},
					wantErr: module.name + ".base_url must be a valid HTTP or HTTPS URL with a host",
				},
				{
					name: "common validation precedes module validation",
					settings: models.ScraperSettings{
						Enabled: true, RateLimit: -1, Language: " FR ", BaseURL: "file:///fixture",
					},
					wantErr: module.name + ": rate_limit must be non-negative, got -1",
				},
				{
					name: "disabled skips invalid runtime settings",
					settings: models.ScraperSettings{
						Enabled: false, RateLimit: -1, Language: " FR ", BaseURL: "file:///fixture",
					},
				},
			}
			if len(module.languages) > 0 {
				cases = append(cases,
					validationCase{name: "blank normalized language", settings: models.ScraperSettings{Enabled: true, Language: " \t\n"}},
					validationCase{
						name: "unsupported normalized language", settings: models.ScraperSettings{Enabled: true, Language: " FR "},
						wantErr: fmt.Sprintf("%s: %s, got %q", module.name, module.languageError, "fr"),
					},
				)
				for _, language := range module.languages {
					cases = append(cases, validationCase{
						name: "normalized language " + language,
						settings: models.ScraperSettings{
							Enabled: true, Language: " \t" + strings.ToUpper(language) + " \n",
						},
					})
				}
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					// Exercise the startup registration and public validation path, not
					// a captured validator closure. No scraper instance is constructed.
					registry := scraperutil.NewScraperRegistry()
					scraper.RegisterAll(registry)
					defaults := registry.GetAllDefaults()
					require.Contains(t, defaults, module.name)
					assert.False(t, defaults[module.name].Enabled)
					assert.Equal(t, module.defaultURL, defaults[module.name].BaseURL)
					assert.Equal(t, module.defaultLanguage, defaults[module.name].Language)

					override := tc.settings.Clone()
					override.Cookies = map[string]string{"fixture": "preserve-me"}
					original := override.Clone()
					cfg := &config.Config{Scrapers: config.ScrapersConfig{
						Overrides: map[string]*models.ScraperSettings{module.name: &override},
					}}
					require.NoError(t, cfg.Scrapers.Finalize(registry))
					resolved := cfg.Scrapers.ResolvedSettings(module.name)
					assert.Equal(t, tc.settings.Enabled, resolved.Enabled)
					if tc.settings.BaseURL == "" {
						assert.Equal(t, module.defaultURL, resolved.BaseURL)
					} else {
						assert.Equal(t, tc.settings.BaseURL, resolved.BaseURL)
					}
					if tc.settings.Language == "" {
						assert.Equal(t, module.defaultLanguage, resolved.Language)
					} else {
						assert.Equal(t, tc.settings.Language, resolved.Language)
					}
					for range 2 {
						err := config.ValidateScraperOverrides(cfg)
						if tc.wantErr == "" {
							require.NoError(t, err)
						} else {
							require.ErrorContains(t, err, tc.wantErr)
						}
						assert.Same(t, &override, cfg.Scrapers.Overrides[module.name])
						assert.Equal(t, original, override, "validation must not normalize or merge defaults into caller overrides")
						assert.Equal(t, resolved, cfg.Scrapers.ResolvedSettings(module.name))
						assert.Equal(t, defaults, registry.GetAllDefaults(), "validation must not rewrite registered defaults")
					}
					assert.Empty(t, registry.GetAllInstances(), "settings validation must not instantiate scrapers")
				})
			}
		})
	}
}
