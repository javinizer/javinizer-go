package config

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestScrapersConfig_MarshalJSON_OmittedEnabledExposesEffectiveDefaultTrue(t *testing.T) {
	t.Parallel()

	sc := ScrapersConfig{Overrides: map[string]*models.ScraperSettings{
		"r18dev": {RateLimit: 500},
	}}
	sc.Overrides["r18dev"].SetEnabledPresence(false)
	require.NoError(t, sc.Finalize(newEnabledResolver()))

	data, err := json.Marshal(&sc)
	require.NoError(t, err)

	raw := map[string]json.RawMessage{}
	require.NoError(t, json.Unmarshal(data, &raw))
	r18, ok := raw["r18dev"]
	require.True(t, ok)

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(r18, &fields))
	assert.JSONEq(t, "true", string(fields["enabled"]), "omitted enabled must expose effective default true")
	assert.JSONEq(t, "500", string(fields["rate_limit"]))

	override := sc.Overrides["r18dev"]
	require.NotNil(t, override)
	assert.False(t, override.Enabled, "stored override must not be mutated")
}

func TestScrapersConfig_MarshalJSON_ExplicitFalsePreserved(t *testing.T) {
	t.Parallel()

	sc := ScrapersConfig{Overrides: map[string]*models.ScraperSettings{
		"r18dev": {Enabled: false, RateLimit: 500},
	}}
	sc.Overrides["r18dev"].SetEnabledPresence(true)
	require.NoError(t, sc.Finalize(newEnabledResolver()))

	data, err := json.Marshal(&sc)
	require.NoError(t, err)

	raw := map[string]json.RawMessage{}
	require.NoError(t, json.Unmarshal(data, &raw))
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw["r18dev"], &fields))
	assert.JSONEq(t, "false", string(fields["enabled"]), "explicit false must be preserved")
}

func TestScrapersConfig_MarshalJSON_OmittedEnabledExposesEffectiveDefaultFalse(t *testing.T) {
	t.Parallel()

	sc := ScrapersConfig{Overrides: map[string]*models.ScraperSettings{
		"dmm": {RateLimit: 250},
	}}
	sc.Overrides["dmm"].SetEnabledPresence(false)
	require.NoError(t, sc.Finalize(newEnabledResolver()))

	data, err := json.Marshal(&sc)
	require.NoError(t, err)

	raw := map[string]json.RawMessage{}
	require.NoError(t, json.Unmarshal(data, &raw))
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw["dmm"], &fields))
	assert.JSONEq(t, "false", string(fields["enabled"]), "omitted enabled must expose effective default false")
}

func TestScrapersConfig_MarshalJSON_ProgrammaticValuePreservedWithoutResolver(t *testing.T) {
	t.Parallel()

	sc := ScrapersConfig{Overrides: map[string]*models.ScraperSettings{
		"r18dev": {Enabled: true, RateLimit: 500},
	}}

	data, err := json.Marshal(&sc)
	require.NoError(t, err)

	raw := map[string]json.RawMessage{}
	require.NoError(t, json.Unmarshal(data, &raw))
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw["r18dev"], &fields))
	assert.JSONEq(t, "true", string(fields["enabled"]), "programmatic value must be preserved when no resolver is set")
}

func TestScrapersConfig_MarshalYAML_PreservesRawOmittedEnabled(t *testing.T) {
	t.Parallel()

	sc := ScrapersConfig{Overrides: map[string]*models.ScraperSettings{
		"r18dev": {RateLimit: 500},
	}}
	sc.Overrides["r18dev"].SetEnabledPresence(false)
	require.NoError(t, sc.Finalize(newEnabledResolver()))

	data, err := yaml.Marshal(&sc)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, yaml.Unmarshal(data, &parsed))
	r18, ok := parsed["r18dev"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, false, r18["enabled"])
	assert.Equal(t, 500, r18["rate_limit"])
}

func TestConfig_RedactThenMarshal_OmittedEnabledExposesEffectiveDefaultTrue(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {RateLimit: 500},
	}
	cfg.Scrapers.Overrides["r18dev"].SetEnabledPresence(false)
	require.NoError(t, cfg.Scrapers.Finalize(newEnabledResolver()))

	redacted := cfg.Redact()
	require.NotNil(t, redacted)

	data, err := json.Marshal(redacted)
	require.NoError(t, err)

	raw := map[string]json.RawMessage{}
	require.NoError(t, json.Unmarshal(data, &raw))
	var scrapers map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw["scrapers"], &scrapers))
	r18, ok := scrapers["r18dev"]
	require.True(t, ok, "r18dev override must be present in API config output")
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(r18, &fields))
	assert.JSONEq(t, "true", string(fields["enabled"]), "API config output must expose effective default true for omitted enabled")
	assert.JSONEq(t, "500", string(fields["rate_limit"]))

	override := cfg.Scrapers.Overrides["r18dev"]
	require.NotNil(t, override)
	assert.False(t, override.Enabled, "stored override must not be mutated by marshal")
}

func TestScrapersConfig_MarshalJSON_OmittedEnabledSaveReloadRoundTrip(t *testing.T) {
	cs := newMemStorage()
	path := "/cfg/config.yaml"
	initial := strings.Join([]string{
		"config_version: 3",
		"scrapers:",
		"    r18dev:",
		"        rate_limit: 500",
	}, "\n")
	require.NoError(t, afero.WriteFile(cs.fs, path, []byte(initial), 0o644))

	loaded, err := cs.Load(path)
	require.NoError(t, err)
	require.NoError(t, loaded.Scrapers.Finalize(newEnabledResolver()))

	data, err := json.Marshal(loaded.Redact())
	require.NoError(t, err)

	roundTripCfg := DefaultConfig(nil, nil)
	require.NoError(t, json.Unmarshal(data, roundTripCfg))
	roundTripCfg.Scrapers.resolver = newEnabledResolver()
	require.NoError(t, roundTripCfg.Scrapers.Finalize(newEnabledResolver()))

	resolved := roundTripCfg.Scrapers.ResolvedSettings("r18dev")
	assert.True(t, resolved.Enabled, "effective enabled must remain true after API round-trip")
	assert.Equal(t, 500, resolved.RateLimit)

	ctx, err := BuildSparseSaveContextWithScrapers([]string{"r18dev", "dmm"}, enabledDefaultsForTest())
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(roundTripCfg, path, ctx))

	got, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	assert.NotContains(t, string(got), "enabled: false", "API round-trip must not persist raw false for omitted enabled")
}

func TestReconcileSparse_RemovedCustomProxyProfileDeleted(t *testing.T) {
	cs := newMemStorage()
	path := "/cfg/config.yaml"
	initial := strings.Join([]string{
		"config_version: 3",
		"scrapers:",
		"    proxy:",
		"        profiles:",
		"            main:",
		"                url: http://main",
		"            custom:",
		"                url: http://custom",
	}, "\n")
	require.NoError(t, afero.WriteFile(cs.fs, path, []byte(initial), 0o644))

	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
		"main": {URL: "http://main"},
	}
	ctx, err := BuildSparseSaveContextWithScrapers(nil, nil)
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	got, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	assert.NotContains(t, string(got), "custom:", "removed custom proxy profile must be deleted from disk")
	assert.Contains(t, string(got), "main:", "retained profile must remain")
}

func TestReconcileSparse_RemovedDownloadProxyProfileDeleted(t *testing.T) {
	cs := newMemStorage()
	path := "/cfg/config.yaml"
	initial := strings.Join([]string{
		"config_version: 3",
		"output:",
		"    download_proxy:",
		"        profiles:",
		"            main:",
		"                url: http://dl-main",
		"            custom-dl:",
		"                url: http://dl-custom",
	}, "\n")
	require.NoError(t, afero.WriteFile(cs.fs, path, []byte(initial), 0o644))

	cfg := DefaultConfig(nil, nil)
	cfg.Output.Download.DownloadProxy.Profiles = map[string]models.ProxyProfile{
		"main": {URL: "http://dl-main"},
	}
	ctx, err := BuildSparseSaveContextWithScrapers(nil, nil)
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	got, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	assert.NotContains(t, string(got), "custom-dl:", "removed download proxy profile must be deleted from disk")
	assert.Contains(t, string(got), "main:", "retained download profile must remain")
}

func TestReconcileSparse_RemovedPerScraperProxyProfileDeleted(t *testing.T) {
	cs := newMemStorage()
	path := "/cfg/config.yaml"
	initial := strings.Join([]string{
		"config_version: 3",
		"scrapers:",
		"    r18dev:",
		"        proxy:",
		"            profiles:",
		"                main:",
		"                    url: http://scraper-main",
		"                custom-scraper:",
		"                    url: http://scraper-custom",
	}, "\n")
	require.NoError(t, afero.WriteFile(cs.fs, path, []byte(initial), 0o644))

	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {
			Proxy: &models.ProxyConfig{
				Profiles: map[string]models.ProxyProfile{
					"main": {URL: "http://scraper-main"},
				},
			},
		},
	}
	ctx, err := BuildSparseSaveContextWithScrapers([]string{"r18dev"}, scraperDefaultsForTest())
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	got, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	assert.NotContains(t, string(got), "custom-scraper:", "removed per-scraper proxy profile must be deleted from disk")
	assert.Contains(t, string(got), "scraper-main", "retained per-scraper profile url must remain")
}

func TestReconcileSparse_RemovedPerScraperDownloadProxyProfileDeleted(t *testing.T) {
	cs := newMemStorage()
	path := "/cfg/config.yaml"
	initial := strings.Join([]string{
		"config_version: 3",
		"scrapers:",
		"    r18dev:",
		"        download_proxy:",
		"            profiles:",
		"                main:",
		"                    url: http://dl-scraper-main",
		"                custom-dl-scraper:",
		"                    url: http://dl-scraper-custom",
	}, "\n")
	require.NoError(t, afero.WriteFile(cs.fs, path, []byte(initial), 0o644))

	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {
			DownloadProxy: &models.ProxyConfig{
				Profiles: map[string]models.ProxyProfile{
					"main": {URL: "http://dl-scraper-main"},
				},
			},
		},
	}
	ctx, err := BuildSparseSaveContextWithScrapers([]string{"r18dev"}, scraperDefaultsForTest())
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	got, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	assert.NotContains(t, string(got), "custom-dl-scraper:", "removed per-scraper download proxy profile must be deleted from disk")
	assert.Contains(t, string(got), "dl-scraper-main", "retained per-scraper download profile url must remain")
}

func TestReconcileSparse_UnknownKeysStillPreservedWithProxyProfileDeletion(t *testing.T) {
	cs := newMemStorage()
	path := "/cfg/config.yaml"
	initial := strings.Join([]string{
		"config_version: 2",
		"my_custom_key: custom_value",
		"scrapers:",
		"    proxy:",
		"        profiles:",
		"            main:",
		"                url: http://main",
		"            gone:",
		"                url: http://gone",
	}, "\n")
	require.NoError(t, afero.WriteFile(cs.fs, path, []byte(initial), 0o644))

	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
		"main": {URL: "http://main"},
	}
	ctx, err := BuildSparseSaveContextWithScrapers(nil, nil)
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	got, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	assert.Contains(t, string(got), "my_custom_key: custom_value", "unrelated unknown top-level key must be preserved")
	assert.NotContains(t, string(got), "gone:", "removed custom proxy profile must still be deleted")
}

func TestIsSourceAuthoritativeFreeFormMap(t *testing.T) {
	cases := map[string]bool{
		"scrapers.proxy.profiles":                 true,
		"output.download_proxy.profiles":          true,
		"scrapers.r18dev.proxy.profiles":          true,
		"scrapers.r18dev.download_proxy.profiles": true,
		"scrapers":               false,
		"scrapers.proxy":         false,
		"scrapers.proxy.enabled": false,
		"output.download.download_proxy.profiles": false,
		"":                        false,
		"metadata.priority":       false,
		"scrapers.r18dev.cookies": false,
	}
	for path, want := range cases {
		assert.Equal(t, want, isSourceAuthoritativeFreeFormMap(path), "path=%q", path)
	}
}

func TestScrapersConfig_MarshalJSON_SkipsNilAndUnknownOverrides(t *testing.T) {
	t.Parallel()

	sc := ScrapersConfig{Overrides: map[string]*models.ScraperSettings{
		"nil":    nil,
		"custom": {Enabled: true},
	}}
	sc.resolver = newEnabledResolver()
	data, err := json.Marshal(&sc)
	require.NoError(t, err)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.NotContains(t, raw, "nil")
	assert.Contains(t, raw, "custom")
}

func TestScrapersConfig_MarshalYAML_SkipsNilOverride(t *testing.T) {
	t.Parallel()

	sc := ScrapersConfig{Overrides: map[string]*models.ScraperSettings{"nil": nil}}
	data, err := yaml.Marshal(&sc)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "nil:")
}

func TestEffectiveOverrideForMarshal_NilSettings(t *testing.T) {
	assert.Nil(t, (&ScrapersConfig{}).effectiveOverrideForMarshal("r18dev", nil, nil))
}
