package config

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOutputDownloadProxy_SaveLoad_MappingReplacesBuiltIns(t *testing.T) {
	t.Parallel()

	cs := newMemStorage()
	path := "/cfg/config.yaml"
	initial := strings.Join([]string{
		"config_version: 3",
		"output:",
		"    download_proxy:",
		"        profiles:",
		"            main:",
		"                url: http://dl-main",
		"            backup:",
		"                url: http://dl-backup",
	}, "\n")
	writeConfigYAML(t, cs, path, initial)

	cfg := DefaultConfig(nil, nil)
	cfg.Output.Download.DownloadProxy.Profiles = map[string]models.ProxyProfile{
		"main":   {URL: "http://dl-main"},
		"custom": {URL: "http://dl-custom"},
	}
	ctx, err := BuildSparseSaveContextWithScrapers(nil, nil)
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	got, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	result := string(got)
	assert.Contains(t, result, "http://dl-main")
	assert.Contains(t, result, "http://dl-custom")
	assert.NotContains(t, result, "backup: null")
	assert.NotContains(t, result, "http://dl-backup")

	loaded, err := cs.Load(path)
	require.NoError(t, err)
	profiles := loaded.Output.Download.DownloadProxy.Profiles
	_, hasMain := profiles["main"]
	_, hasCustom := profiles["custom"]
	_, hasBackup := profiles["backup"]
	assert.True(t, hasMain)
	assert.True(t, hasCustom)
	assert.False(t, hasBackup, "removed download_proxy built-in must not be restored on reload")
	assert.Equal(t, "http://dl-main", profiles["main"].URL)
}

func TestOutputDownloadProxy_SaveLoad_TombstoneStripped(t *testing.T) {
	t.Parallel()

	cs := newMemStorage()
	path := "/cfg/config.yaml"
	initial := strings.Join([]string{
		"config_version: 3",
		"output:",
		"    download_proxy:",
		"        profiles:",
		"            main:",
		"                url: http://dl-main",
		"            backup: null",
		"            custom:",
		"                url: http://dl-custom",
	}, "\n")
	writeConfigYAML(t, cs, path, initial)

	cfg := DefaultConfig(nil, nil)
	cfg.Output.Download.DownloadProxy.Profiles = map[string]models.ProxyProfile{
		"main":   {URL: "http://dl-main"},
		"custom": {URL: "http://dl-custom"},
	}
	ctx, err := BuildSparseSaveContextWithScrapers(nil, nil)
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	got, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	result := string(got)
	assert.NotContains(t, result, "backup: null", "tombstone entry must be stripped on save")
	assert.NotContains(t, result, "backup:")

	loaded, err := cs.Load(path)
	require.NoError(t, err)
	profiles := loaded.Output.Download.DownloadProxy.Profiles
	_, hasMain := profiles["main"]
	_, hasCustom := profiles["custom"]
	_, hasBackup := profiles["backup"]
	assert.True(t, hasMain)
	assert.True(t, hasCustom)
	assert.False(t, hasBackup, "tombstone profile must not materialise on reload")
}

func TestOutputDownloadProxy_SaveLoad_AliasedProfilesResolved(t *testing.T) {
	t.Parallel()

	cs := newMemStorage()
	path := "/cfg/config.yaml"
	initial := strings.Join([]string{
		"config_version: 3",
		"output:",
		"    download_proxy:",
		"        profiles:",
		"            main: &main",
		"                url: http://dl-main",
		"            backup: *main",
	}, "\n")
	writeConfigYAML(t, cs, path, initial)

	cfg := DefaultConfig(nil, nil)
	cfg.Output.Download.DownloadProxy.Profiles = map[string]models.ProxyProfile{
		"main": {URL: "http://dl-main"},
	}
	ctx, err := BuildSparseSaveContextWithScrapers(nil, nil)
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	got, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	result := string(got)
	assert.Contains(t, result, "http://dl-main")
	assert.NotContains(t, result, "backup:")
	assert.NotContains(t, result, "backup: null")
	assert.Equal(t, 1, strings.Count(result, "main:"))

	loaded, err := cs.Load(path)
	require.NoError(t, err)
	profiles := loaded.Output.Download.DownloadProxy.Profiles
	_, hasMain := profiles["main"]
	_, hasBackup := profiles["backup"]
	assert.True(t, hasMain, "retained aliased profile must persist")
	assert.False(t, hasBackup, "deleted aliased built-in must be absent after reload")
}

func TestPerScraperDownloadProxy_SaveLoad_PartialReplacesExactly(t *testing.T) {
	t.Parallel()

	cs := newMemStorage()
	path := "/cfg/config.yaml"
	initial := strings.Join([]string{
		"config_version: 3",
		"scrapers:",
		"    r18dev:",
		"        download_proxy:",
		"            profiles:",
		"                seed:",
		"                    url: http://dl-seed",
		"                extra:",
		"                    url: http://dl-extra",
	}, "\n")
	writeConfigYAML(t, cs, path, initial)

	scraperDefaults := map[string]models.ScraperSettings{
		"r18dev": {DownloadProxy: &models.ProxyConfig{Profiles: map[string]models.ProxyProfile{
			"seed":  {URL: ""},
			"extra": {URL: ""},
		}}},
	}
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {DownloadProxy: &models.ProxyConfig{Profiles: map[string]models.ProxyProfile{
			"seed": {URL: "http://dl-seed"},
		}}},
	}
	ctx, err := BuildSparseSaveContextWithScrapers([]string{"r18dev"}, scraperDefaults)
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	got, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	result := string(got)
	assert.Contains(t, result, "http://dl-seed")
	assert.NotContains(t, result, "extra:")
	assert.NotContains(t, result, "extra: null")

	loaded, err := cs.Load(path)
	require.NoError(t, err)
	r18, ok := loaded.Scrapers.Overrides["r18dev"]
	require.True(t, ok)
	require.NotNil(t, r18.DownloadProxy)
	_, hasSeed := r18.DownloadProxy.Profiles["seed"]
	_, hasExtra := r18.DownloadProxy.Profiles["extra"]
	assert.True(t, hasSeed, "retained per-scraper download_proxy profile must survive round-trip")
	assert.False(t, hasExtra, "deleted per-scraper download_proxy profile must not be restored on reload")
}

func TestProxyProfiles_JSON_GlobalProxyReusedStruct(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		json     string
		wantKeys []string
		notKeys  []string
	}{
		{"omitted profiles retain built-ins", `{"scrapers":{"proxy":{"enabled":true}}}`, []string{"main", "backup"}, nil},
		{"mapping replaces built-ins", `{"scrapers":{"proxy":{"profiles":{"main":{"url":"http://x"},"custom":{"url":"http://c"}}}}}`, []string{"main", "custom"}, []string{"backup"}},
		{"null clears built-ins", `{"scrapers":{"proxy":{"profiles":null}}}`, nil, []string{"main", "backup"}},
		{"tombstone stripped", `{"scrapers":{"proxy":{"profiles":{"main":{"url":"http://x"},"backup":null}}}}`, []string{"main"}, []string{"backup"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := DefaultConfig(nil, nil)
			require.NoError(t, json.Unmarshal([]byte(tc.json), cfg))
			profiles := cfg.Scrapers.Proxy.Profiles
			for _, k := range tc.wantKeys {
				_, ok := profiles[k]
				assert.Truef(t, ok, "expected profile %q", k)
			}
			for _, k := range tc.notKeys {
				_, ok := profiles[k]
				assert.Falsef(t, ok, "profile %q must be absent", k)
			}
		})
	}
}

func TestProxyProfiles_JSON_OutputDownloadProxyReusedStruct(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		json     string
		wantKeys []string
		notKeys  []string
	}{
		{"omitted profiles retain built-ins", `{"output":{"download_proxy":{"enabled":true}}}`, []string{"main", "backup"}, nil},
		{"mapping replaces built-ins", `{"output":{"download_proxy":{"profiles":{"main":{"url":"http://x"},"custom":{"url":"http://c"}}}}}`, []string{"main", "custom"}, []string{"backup"}},
		{"null clears built-ins", `{"output":{"download_proxy":{"profiles":null}}}`, nil, []string{"main", "backup"}},
		{"tombstone stripped", `{"output":{"download_proxy":{"profiles":{"main":{"url":"http://x"},"backup":null}}}}`, []string{"main"}, []string{"backup"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := DefaultConfig(nil, nil)
			cfg.Output.Download.DownloadProxy.Profiles = map[string]models.ProxyProfile{
				"main":   {},
				"backup": {},
			}
			require.NoError(t, json.Unmarshal([]byte(tc.json), cfg))
			profiles := cfg.Output.Download.DownloadProxy.Profiles
			for _, k := range tc.wantKeys {
				_, ok := profiles[k]
				assert.Truef(t, ok, "expected profile %q", k)
			}
			for _, k := range tc.notKeys {
				_, ok := profiles[k]
				assert.Falsef(t, ok, "profile %q must be absent", k)
			}
		})
	}
}

func TestProxyProfiles_JSON_PerScraperProxyReusedStruct(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		json     string
		wantKeys []string
		notKeys  []string
	}{
		{"omitted profiles retain pre-seeded", `{"proxy":{"enabled":true}}`, []string{"main", "backup"}, nil},
		{"mapping replaces pre-seeded", `{"proxy":{"profiles":{"main":{"url":"http://x"},"custom":{"url":"http://c"}}}}`, []string{"main", "custom"}, []string{"backup"}},
		{"null clears pre-seeded", `{"proxy":{"profiles":null}}`, nil, []string{"main", "backup"}},
		{"tombstone stripped", `{"proxy":{"profiles":{"main":{"url":"http://x"},"backup":null}}}`, []string{"main"}, []string{"backup"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ss := models.ScraperSettings{Proxy: &models.ProxyConfig{Profiles: map[string]models.ProxyProfile{
				"main":   {URL: "seed-main"},
				"backup": {URL: "seed-backup"},
			}}}
			require.NoError(t, json.Unmarshal([]byte(tc.json), &ss))
			require.NotNil(t, ss.Proxy)
			profiles := ss.Proxy.Profiles
			for _, k := range tc.wantKeys {
				_, ok := profiles[k]
				assert.Truef(t, ok, "expected profile %q", k)
			}
			for _, k := range tc.notKeys {
				_, ok := profiles[k]
				assert.Falsef(t, ok, "profile %q must be absent", k)
			}
		})
	}
}

func TestProxyProfiles_JSON_PerScraperDownloadProxyReusedStruct(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		json     string
		wantKeys []string
		notKeys  []string
	}{
		{"omitted profiles retain pre-seeded", `{"download_proxy":{"enabled":true}}`, []string{"main", "backup"}, nil},
		{"mapping replaces pre-seeded", `{"download_proxy":{"profiles":{"main":{"url":"http://x"},"custom":{"url":"http://c"}}}}`, []string{"main", "custom"}, []string{"backup"}},
		{"null clears pre-seeded", `{"download_proxy":{"profiles":null}}`, nil, []string{"main", "backup"}},
		{"tombstone stripped", `{"download_proxy":{"profiles":{"main":{"url":"http://x"},"backup":null}}}`, []string{"main"}, []string{"backup"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ss := models.ScraperSettings{DownloadProxy: &models.ProxyConfig{Profiles: map[string]models.ProxyProfile{
				"main":   {URL: "seed-main"},
				"backup": {URL: "seed-backup"},
			}}}
			require.NoError(t, json.Unmarshal([]byte(tc.json), &ss))
			require.NotNil(t, ss.DownloadProxy)
			profiles := ss.DownloadProxy.Profiles
			for _, k := range tc.wantKeys {
				_, ok := profiles[k]
				assert.Truef(t, ok, "expected profile %q", k)
			}
			for _, k := range tc.notKeys {
				_, ok := profiles[k]
				assert.Falsef(t, ok, "profile %q must be absent", k)
			}
		})
	}
}
