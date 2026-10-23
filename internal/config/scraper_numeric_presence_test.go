package config

import (
	"encoding/json"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNumericPresenceSaveReload(t *testing.T) {
	for _, input := range []struct {
		name, fields string
		want         [3]int
	}{
		{"omitted", "language: en", [3]int{500, 30, 3}},
		{"zero", "rate_limit: 0\n        timeout: 0\n        retry_count: 0", [3]int{}},
		{"nonzero", "rate_limit: 100\n        timeout: 20\n        retry_count: 2", [3]int{100, 20, 2}},
		{"legacy zero", "request_delay: 0\n        timeout: 0\n        max_retries: 0", [3]int{}},
		{"canonical wins", "request_delay: 100\n        rate_limit: 0\n        timeout: 0\n        max_retries: 2\n        retry_count: 0", [3]int{}},
		{"null", "rate_limit: null\n        timeout: null\n        retry_count: null", [3]int{500, 30, 3}},
	} {
		t.Run(input.name, func(t *testing.T) {
			defaults := map[string]models.ScraperSettings{"r18dev": {Enabled: true, Language: "en", RateLimit: 500, Timeout: 30, RetryCount: 3}}
			resolver := &staticTestConfigResolver{registered: map[string]bool{"r18dev": true}, defaults: defaults}
			cs := newMemStorage()
			path := "/cfg/config.yaml"
			require.NoError(t, afero.WriteFile(cs.fs, path, []byte("config_version: 3\nscrapers:\n    r18dev:\n        "+input.fields+"\n"), 0600))
			loaded, err := cs.Load(path)
			require.NoError(t, err)
			require.NoError(t, loaded.Scrapers.Finalize(resolver))
			check := func(cfg *Config) {
				resolved := cfg.Scrapers.ResolvedSettings("r18dev")
				assert.Equal(t, input.want, [3]int{resolved.RateLimit, resolved.Timeout, resolved.RetryCount})
				assert.True(t, resolved.Enabled)
			}
			check(loaded)
			data, err := json.Marshal(loaded.Redact())
			require.NoError(t, err)
			edited := DefaultConfig(nil, nil)
			require.NoError(t, json.Unmarshal(data, edited))
			require.NoError(t, edited.Scrapers.Finalize(resolver))
			check(edited)
			ctx, err := BuildSparseSaveContextWithScrapers([]string{"r18dev"}, defaults)
			require.NoError(t, err)
			require.NoError(t, cs.SaveSparse(edited, path, ctx))
			saved, err := afero.ReadFile(cs.fs, path)
			require.NoError(t, err)
			assert.NotContains(t, string(saved), "request_delay:")
			assert.NotContains(t, string(saved), "max_retries:")
			if input.want == [3]int{} {
				assert.Contains(t, string(saved), "rate_limit: 0")
				assert.Contains(t, string(saved), "timeout: 0")
				assert.Contains(t, string(saved), "retry_count: 0")
			}
			reloaded, err := cs.Load(path)
			require.NoError(t, err)
			require.NoError(t, reloaded.Scrapers.Finalize(resolver))
			check(reloaded)
		})
	}
}

func TestNumericPresenceRejectsUnknownAndInvalidAliases(t *testing.T) {
	for _, input := range []string{`{"r18dev":{"unknown":0}}`, `{"r18dev":{"request_delay":0,"unknown":0}}`, `{"r18dev":{"request_delay":"bad"}}`, `{"r18dev":{"max_retries":[]}}`} {
		var sc ScrapersConfig
		require.Error(t, json.Unmarshal([]byte(input), &sc))
	}
	for _, input := range []string{"r18dev: {request_delay: bad}", "r18dev: {max_retries: []}", "r18dev: {rate_limit: 0, unknown: 0}"} {
		var sc ScrapersConfig
		require.Error(t, yaml.Unmarshal([]byte(input), &sc))
	}
}
