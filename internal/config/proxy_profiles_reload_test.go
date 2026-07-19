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

func writeConfigYAML(t *testing.T, cs *ConfigStorage, path, body string) {
	t.Helper()
	require.NoError(t, afero.WriteFile(cs.fs, path, []byte(body), 0o644))
}

func TestProxyProfilesReload_PartialGlobalMapDeletesBuiltIn(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		yaml     string
		wantKeys []string
		notKeys  []string
	}{
		{
			name: "backup deleted, main and custom retained",
			yaml: strings.Join([]string{
				"config_version: 3",
				"scrapers:",
				"    proxy:",
				"        profiles:",
				"            main:",
				"                url: http://main",
				"            custom:",
				"                url: http://custom",
			}, "\n"),
			wantKeys: []string{"main", "custom"},
			notKeys:  []string{"backup"},
		},
		{
			name: "main deleted, backup and custom retained",
			yaml: strings.Join([]string{
				"config_version: 3",
				"scrapers:",
				"    proxy:",
				"        profiles:",
				"            backup:",
				"                url: http://backup",
			}, "\n"),
			wantKeys: []string{"backup"},
			notKeys:  []string{"main", "custom"},
		},
		{
			name: "both built-ins deleted, custom retained",
			yaml: strings.Join([]string{
				"config_version: 3",
				"scrapers:",
				"    proxy:",
				"        profiles:",
				"            custom:",
				"                url: http://custom",
			}, "\n"),
			wantKeys: []string{"custom"},
			notKeys:  []string{"main", "backup"},
		},
		{
			name: "only main retained, backup and custom deleted",
			yaml: strings.Join([]string{
				"config_version: 3",
				"scrapers:",
				"    proxy:",
				"        profiles:",
				"            main:",
				"                url: http://main",
			}, "\n"),
			wantKeys: []string{"main"},
			notKeys:  []string{"backup", "custom"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cs := newMemStorage()
			path := "/cfg/config.yaml"
			writeConfigYAML(t, cs, path, tc.yaml)

			loaded, err := cs.Load(path)
			require.NoError(t, err)
			profiles := loaded.Scrapers.Proxy.Profiles
			for _, k := range tc.wantKeys {
				_, ok := profiles[k]
				assert.Truef(t, ok, "expected profile %q to survive reload", k)
			}
			for _, k := range tc.notKeys {
				_, ok := profiles[k]
				assert.Falsef(t, ok, "deleted built-in/default profile %q must not be restored on reload", k)
			}
		})
	}
}

func TestProxyProfilesReload_OmittedProfilesRetainsDefaults(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		yaml string
	}{
		{
			name: "proxy present without profiles key",
			yaml: strings.Join([]string{
				"config_version: 3",
				"scrapers:",
				"    proxy:",
				"        enabled: false",
			}, "\n"),
		},
		{
			name: "scrapers present without proxy key",
			yaml: strings.Join([]string{
				"config_version: 3",
				"scrapers:",
				"    priority:",
				"        - r18dev",
			}, "\n"),
		},
		{
			name: "empty config (no scrapers)",
			yaml: "config_version: 3\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cs := newMemStorage()
			path := "/cfg/config.yaml"
			writeConfigYAML(t, cs, path, tc.yaml)

			loaded, err := cs.Load(path)
			require.NoError(t, err)
			profiles := loaded.Scrapers.Proxy.Profiles
			_, hasMain := profiles["main"]
			_, hasBackup := profiles["backup"]
			assert.True(t, hasMain, "omitted profiles must retain built-in default main")
			assert.True(t, hasBackup, "omitted profiles must retain built-in default backup")
		})
	}
}

func TestProxyProfilesReload_NullProfilesClearsAll(t *testing.T) {
	t.Parallel()

	cs := newMemStorage()
	path := "/cfg/config.yaml"
	writeConfigYAML(t, cs, path, strings.Join([]string{
		"config_version: 3",
		"scrapers:",
		"    proxy:",
		"        profiles: null",
	}, "\n"))

	loaded, err := cs.Load(path)
	require.NoError(t, err)
	assert.Empty(t, loaded.Scrapers.Proxy.Profiles, "explicit null profiles must clear pre-seeded defaults")
}

func TestProxyProfilesReload_NullTombstoneEntriesStripped(t *testing.T) {
	t.Parallel()

	cs := newMemStorage()
	path := "/cfg/config.yaml"
	writeConfigYAML(t, cs, path, strings.Join([]string{
		"config_version: 3",
		"scrapers:",
		"    proxy:",
		"        default_profile: main",
		"        profiles:",
		"            main:",
		"                url: http://main",
		"            backup: null",
		"            custom:",
		"                url: http://custom",
	}, "\n"))

	loaded, err := cs.Load(path)
	require.NoError(t, err)
	profiles := loaded.Scrapers.Proxy.Profiles
	_, hasMain := profiles["main"]
	_, hasCustom := profiles["custom"]
	_, hasBackup := profiles["backup"]
	assert.True(t, hasMain, "explicit mapping profile must survive")
	assert.True(t, hasCustom, "custom profile must survive")
	assert.False(t, hasBackup, "null tombstone entry must be dropped, not materialised as zero-value profile")
	assert.Equal(t, "http://main", profiles["main"].URL)
	assert.Equal(t, "http://custom", profiles["custom"].URL)
}

func TestProxyProfilesReload_DownloadProxyNullClearsAll(t *testing.T) {
	t.Parallel()

	cs := newMemStorage()
	path := "/cfg/config.yaml"
	writeConfigYAML(t, cs, path, strings.Join([]string{
		"config_version: 3",
		"output:",
		"    download_proxy:",
		"        profiles: null",
	}, "\n"))

	loaded, err := cs.Load(path)
	require.NoError(t, err)
	assert.Empty(t, loaded.Output.Download.DownloadProxy.Profiles, "explicit null download_proxy profiles must clear")
}

func TestProxyProfilesReload_PerScraperPartialReplacesExactly(t *testing.T) {
	t.Parallel()

	cs := newMemStorage()
	path := "/cfg/config.yaml"
	writeConfigYAML(t, cs, path, strings.Join([]string{
		"config_version: 3",
		"scrapers:",
		"    r18dev:",
		"        proxy:",
		"            profiles:",
		"                seed:",
		"                    url: http://seed",
		"                extra: null",
	}, "\n"))

	loaded, err := cs.Load(path)
	require.NoError(t, err)
	r18, ok := loaded.Scrapers.Overrides["r18dev"]
	require.True(t, ok, "r18dev override must persist")
	require.NotNil(t, r18.Proxy)
	_, hasSeed := r18.Proxy.Profiles["seed"]
	_, hasExtra := r18.Proxy.Profiles["extra"]
	assert.True(t, hasSeed, "retained per-scraper profile must survive")
	assert.False(t, hasExtra, "null tombstone per-scraper profile must be dropped")
}

func TestProxyProfilesReload_SaveDeleteThenReloadRoundTrip(t *testing.T) {
	t.Parallel()

	cs := newMemStorage()
	path := "/cfg/config.yaml"
	writeConfigYAML(t, cs, path, strings.Join([]string{
		"config_version: 3",
		"scrapers:",
		"    proxy:",
		"        profiles:",
		"            main:",
		"                url: http://main",
		"            backup:",
		"                url: http://backup",
		"            custom:",
		"                url: http://custom",
	}, "\n"))

	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
		"main":   {URL: "http://main"},
		"custom": {URL: "http://custom"},
	}
	ctx, err := BuildSparseSaveContextWithScrapers(nil, nil)
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	got, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	disk := string(got)
	assert.Contains(t, disk, "main:")
	assert.Contains(t, disk, "custom:")
	assert.NotContains(t, disk, "backup:")
	assert.NotContains(t, disk, "backup: null")

	loaded, err := cs.Load(path)
	require.NoError(t, err)
	profiles := loaded.Scrapers.Proxy.Profiles
	_, hasMain := profiles["main"]
	_, hasCustom := profiles["custom"]
	_, hasBackup := profiles["backup"]
	assert.True(t, hasMain, "retained profile must survive round-trip")
	assert.True(t, hasCustom, "custom profile must survive round-trip")
	assert.False(t, hasBackup, "deleted built-in default must not be restored on reload")
	assert.Equal(t, "http://main", profiles["main"].URL)
	assert.Equal(t, "http://custom", profiles["custom"].URL)
}

func TestProxyProfilesReload_JSONFreshConfigDoesNotRestoreBuiltIns(t *testing.T) {
	t.Parallel()

	partialJSON := []byte(`{"scrapers":{"proxy":{"profiles":{"main":{"url":"http://main"}}}}}`)

	var fresh Config
	require.NoError(t, json.Unmarshal(partialJSON, &fresh))
	profiles := fresh.Scrapers.Proxy.Profiles
	_, hasMain := profiles["main"]
	_, hasBackup := profiles["backup"]
	assert.True(t, hasMain, "explicit profile must survive JSON decode")
	assert.False(t, hasBackup, "API-style JSON decode into a fresh config must not restore built-in defaults")
	assert.Equal(t, "http://main", profiles["main"].URL)
}
