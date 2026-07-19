package config

import (
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestReconcileSparse_NullMarkerTreatedAsDeletionForFreeFormMaps(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		initial         string
		profiles        map[string]models.ProxyProfile
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "removed built-in default profile retains sibling",
			initial: strings.Join([]string{
				"config_version: 3",
				"scrapers:",
				"    proxy:",
				"        profiles:",
				"            main:",
				"                url: http://main",
				"            backup:",
				"                url: http://backup",
			}, "\n"),
			profiles:     map[string]models.ProxyProfile{"main": {URL: "http://main"}},
			wantContains: []string{"main:", "http://main"},
			wantNotContains: []string{
				"backup:",
				"backup: null",
				"http://backup",
			},
		},
		{
			name: "removed built-in default not on disk adds no null marker",
			initial: strings.Join([]string{
				"config_version: 3",
				"scrapers:",
				"    proxy:",
				"        profiles:",
				"            main:",
				"                url: http://main",
			}, "\n"),
			profiles:     map[string]models.ProxyProfile{"main": {URL: "http://main"}},
			wantContains: []string{"main:", "http://main"},
			wantNotContains: []string{
				"backup:",
				"backup: null",
			},
		},
		{
			name: "all built-in defaults removed retains custom sibling",
			initial: strings.Join([]string{
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
			}, "\n"),
			profiles:     map[string]models.ProxyProfile{"custom": {URL: "http://custom"}},
			wantContains: []string{"custom:", "http://custom"},
			wantNotContains: []string{
				"main:",
				"backup:",
				"main: null",
				"backup: null",
				"http://main",
				"http://backup",
			},
		},
		{
			name: "custom profile deletion alongside removed built-in default",
			initial: strings.Join([]string{
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
			}, "\n"),
			profiles:     map[string]models.ProxyProfile{"main": {URL: "http://main"}},
			wantContains: []string{"main:", "http://main"},
			wantNotContains: []string{
				"backup:",
				"backup: null",
				"custom:",
				"http://custom",
				"http://backup",
			},
		},
		{
			name: "unknown top-level key preserved with removed built-in default",
			initial: strings.Join([]string{
				"config_version: 2",
				"my_custom_key: custom_value",
				"scrapers:",
				"    proxy:",
				"        profiles:",
				"            main:",
				"                url: http://main",
				"            backup:",
				"                url: http://backup",
			}, "\n"),
			profiles:     map[string]models.ProxyProfile{"main": {URL: "http://main"}},
			wantContains: []string{"my_custom_key: custom_value", "main:", "http://main"},
			wantNotContains: []string{
				"backup:",
				"backup: null",
			},
		},
		{
			name: "both built-in defaults removed with no profiles on disk",
			initial: strings.Join([]string{
				"config_version: 3",
				"scrapers:",
				"    proxy:",
				"        default_profile: main",
			}, "\n"),
			profiles:     map[string]models.ProxyProfile{"custom": {URL: "http://custom"}},
			wantContains: []string{"custom:", "http://custom"},
			wantNotContains: []string{
				"main: null",
				"backup: null",
				"http://main",
				"http://backup",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cs := newMemStorage()
			path := "/cfg/config.yaml"
			require.NoError(t, afero.WriteFile(cs.fs, path, []byte(tc.initial), 0o644))

			cfg := DefaultConfig(nil, nil)
			cfg.Scrapers.Proxy.Profiles = tc.profiles
			ctx, err := BuildSparseSaveContextWithScrapers(nil, nil)
			require.NoError(t, err)
			require.NoError(t, cs.SaveSparse(cfg, path, ctx))

			got, err := afero.ReadFile(cs.fs, path)
			require.NoError(t, err)
			result := string(got)
			for _, want := range tc.wantContains {
				assert.Contains(t, result, want, "expected %q in result", want)
			}
			for _, notWant := range tc.wantNotContains {
				assert.NotContains(t, result, notWant, "expected %q absent from result", notWant)
			}
		})
	}
}

func TestReconcileSparse_NullMarkerPerScraperProxyProfileDeleted(t *testing.T) {
	t.Parallel()

	cs := newMemStorage()
	path := "/cfg/config.yaml"
	initial := strings.Join([]string{
		"config_version: 3",
		"scrapers:",
		"    r18dev:",
		"        proxy:",
		"            profiles:",
		"                seed:",
		"                    url: http://seed",
		"                extra:",
		"                    url: http://extra",
	}, "\n")
	require.NoError(t, afero.WriteFile(cs.fs, path, []byte(initial), 0o644))

	scraperDefaults := map[string]models.ScraperSettings{
		"r18dev": {
			Proxy: &models.ProxyConfig{
				Profiles: map[string]models.ProxyProfile{
					"seed":  {URL: ""},
					"extra": {URL: ""},
				},
			},
		},
	}
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {
			Proxy: &models.ProxyConfig{
				Profiles: map[string]models.ProxyProfile{"seed": {URL: "http://seed"}},
			},
		},
	}
	ctx, err := BuildSparseSaveContextWithScrapers([]string{"r18dev"}, scraperDefaults)
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	got, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	result := string(got)
	assert.Contains(t, result, "http://seed", "retained seeded per-scraper profile must remain")
	assert.NotContains(t, result, "extra:", "removed per-scraper profile must be deleted")
	assert.NotContains(t, result, "extra: null", "removed built-in default per-scraper profile must not be emitted as null")
}

func TestIsNullScalar(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		node *yaml.Node
		want bool
	}{
		{"explicit null tag", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, true},
		{"null tag with tilde value", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "~"}, true},
		{"null tag with empty value", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: ""}, true},
		{"string scalar", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "null"}, false},
		{"int scalar", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "0"}, false},
		{"mapping node", &yaml.Node{Kind: yaml.MappingNode, Tag: "!!null"}, false},
		{"nil node", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, isNullScalar(tc.node))
		})
	}
}

func TestReconcileSparse_NullMarkerProfilesNullToRetainedOrCustom(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		profiles   map[string]models.ProxyProfile
		wantKeep   string
		wantDelete string
	}{
		{
			name:       "existing profiles null retains one default",
			profiles:   map[string]models.ProxyProfile{"seed": {URL: "http://seed"}},
			wantKeep:   "seed",
			wantDelete: "extra",
		},
		{
			name:       "existing profiles null to one custom profile",
			profiles:   map[string]models.ProxyProfile{"custom": {URL: "http://custom"}},
			wantKeep:   "custom",
			wantDelete: "extra",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cs := newMemStorage()
			path := "/cfg/config.yaml"
			initial := strings.Join([]string{
				"config_version: 3",
				"scrapers:",
				"    r18dev:",
				"        proxy:",
				"            profiles: null",
			}, "\n")
			require.NoError(t, afero.WriteFile(cs.fs, path, []byte(initial), 0o644))

			scraperDefaults := map[string]models.ScraperSettings{
				"r18dev": {Proxy: &models.ProxyConfig{Profiles: map[string]models.ProxyProfile{
					"seed":  {URL: ""},
					"extra": {URL: ""},
				}}},
			}
			cfg := DefaultConfig(nil, nil)
			cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
				"r18dev": {Proxy: &models.ProxyConfig{Profiles: tc.profiles}},
			}
			ctx, err := BuildSparseSaveContextWithScrapers([]string{"r18dev"}, scraperDefaults)
			require.NoError(t, err)
			require.NoError(t, cs.SaveSparse(cfg, path, ctx))

			got, err := afero.ReadFile(cs.fs, path)
			require.NoError(t, err)
			result := string(got)
			assert.Contains(t, result, tc.wantKeep+":")
			assert.NotContains(t, result, tc.wantDelete+":")
			assert.NotContains(t, result, tc.wantDelete+": null")

			loaded, err := cs.Load(path)
			require.NoError(t, err)
			r18, ok := loaded.Scrapers.Overrides["r18dev"]
			require.True(t, ok, "r18dev override should persist")
			require.NotNil(t, r18.Proxy)
			_, hasKeep := r18.Proxy.Profiles[tc.wantKeep]
			assert.True(t, hasKeep, "retained profile %q should be present after reload", tc.wantKeep)
			_, hasDeleted := r18.Proxy.Profiles[tc.wantDelete]
			assert.False(t, hasDeleted, "deleted default %q should be absent after reload", tc.wantDelete)
		})
	}
}

func TestReconcileSparse_NullMarkerAliasedProfilesMapping(t *testing.T) {
	t.Parallel()

	cs := newMemStorage()
	path := "/cfg/config.yaml"
	initial := strings.Join([]string{
		"config_version: 3",
		"scrapers:",
		"    r18dev:",
		"        proxy:",
		"            profiles:",
		"                seed: &seed",
		"                    url: http://seed",
		"                extra: *seed",
	}, "\n")
	require.NoError(t, afero.WriteFile(cs.fs, path, []byte(initial), 0o644))

	scraperDefaults := map[string]models.ScraperSettings{
		"r18dev": {Proxy: &models.ProxyConfig{Profiles: map[string]models.ProxyProfile{
			"seed":  {URL: ""},
			"extra": {URL: ""},
		}}},
	}
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Proxy: &models.ProxyConfig{Profiles: map[string]models.ProxyProfile{"seed": {URL: "http://seed"}}}},
	}
	ctx, err := BuildSparseSaveContextWithScrapers([]string{"r18dev"}, scraperDefaults)
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	got, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	result := string(got)
	assert.Contains(t, result, "http://seed")
	assert.NotContains(t, result, "extra:")
	assert.NotContains(t, result, "extra: null")
	assert.Equal(t, 1, strings.Count(result, "seed:"))

	loaded, err := cs.Load(path)
	require.NoError(t, err)
	r18, ok := loaded.Scrapers.Overrides["r18dev"]
	require.True(t, ok)
	require.NotNil(t, r18.Proxy)
	_, hasSeed := r18.Proxy.Profiles["seed"]
	assert.True(t, hasSeed, "retained aliased profile should persist")
	_, hasExtra := r18.Proxy.Profiles["extra"]
	assert.False(t, hasExtra, "deleted aliased default should be absent after reload")
}

func TestReconcileMappings_NonMappingDestinationStripsFreeFormNulls(t *testing.T) {
	t.Parallel()

	dst := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
	src := mustParseYAML(t, "main:\n    url: http://main\nbackup: null\ncustom:\n    url: http://custom\n")
	reconcileMappings(dst, mappingRoot(src), nil, nil, "scrapers.proxy.profiles")

	require.Equal(t, yaml.MappingNode, dst.Kind)
	assert.NotEqual(t, -1, findMappingValueIndex(dst, "main"))
	assert.NotEqual(t, -1, findMappingValueIndex(dst, "custom"))
	assert.Equal(t, -1, findMappingValueIndex(dst, "backup"))
}

func TestReconcileSparse_DeleteAllProfilesExistingSubtree(t *testing.T) {
	t.Parallel()

	cs := newMemStorage()
	path := "/cfg/config.yaml"
	initial := strings.Join([]string{
		"config_version: 3",
		"scrapers:",
		"    proxy:",
		"        profiles:",
		"            main:",
		"                url: http://main",
		"            backup:",
		"                url: http://backup",
	}, "\n")
	require.NoError(t, afero.WriteFile(cs.fs, path, []byte(initial), 0o644))

	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Proxy.Profiles = nil
	ctx, err := BuildSparseSaveContextWithScrapers(nil, nil)
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	got, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	result := string(got)
	assert.Equal(t, 1, strings.Count(result, "profiles:"))
	assert.Contains(t, result, "profiles: null")
	assert.NotContains(t, result, "main:")
	assert.NotContains(t, result, "backup:")

	loaded, err := cs.Load(path)
	require.NoError(t, err)
	assert.Empty(t, loaded.Scrapers.Proxy.Profiles)
}

func TestReconcileSparse_DeleteAllProfilesAbsentSubtree(t *testing.T) {
	t.Parallel()

	cs := newMemStorage()
	path := "/cfg/config.yaml"
	initial := strings.Join([]string{
		"config_version: 3",
		"scrapers:",
		"    proxy:",
		"        enabled: false",
	}, "\n")
	require.NoError(t, afero.WriteFile(cs.fs, path, []byte(initial), 0o644))

	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{}
	ctx, err := BuildSparseSaveContextWithScrapers(nil, nil)
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	got, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	result := string(got)
	assert.Equal(t, 1, strings.Count(result, "profiles:"))
	assert.Contains(t, result, "profiles: null")

	loaded, err := cs.Load(path)
	require.NoError(t, err)
	assert.Empty(t, loaded.Scrapers.Proxy.Profiles)
}
