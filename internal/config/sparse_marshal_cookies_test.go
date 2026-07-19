package config

import (
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconcileSparse_RemovedScraperCookieDeleted(t *testing.T) {
	cs := newMemStorage()
	path := "/cfg/config.yaml"
	initial := strings.Join([]string{
		"config_version: 3",
		"scrapers:",
		"    r18dev:",
		"        cookies:",
		"            session: keep-me",
		"            age: drop-me",
	}, "\n")
	require.NoError(t, afero.WriteFile(cs.fs, path, []byte(initial), 0o644))

	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Cookies: map[string]string{"session": "keep-me"}},
	}
	ctx, err := BuildSparseSaveContextWithScrapers([]string{"r18dev"}, scraperDefaultsForTest())
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	got, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	assert.Contains(t, string(got), "keep-me", "retained scraper cookie must remain on disk")
	assert.NotContains(t, string(got), "drop-me", "removed scraper cookie must be deleted from disk")
}

func TestReconcileSparse_RemovedAllScraperCookiesDeletesCookiesBlock(t *testing.T) {
	cs := newMemStorage()
	path := "/cfg/config.yaml"
	initial := strings.Join([]string{
		"config_version: 3",
		"scrapers:",
		"    r18dev:",
		"        rate_limit: 500",
		"        cookies:",
		"            session: drop-me",
		"            age: also-drop-me",
	}, "\n")
	require.NoError(t, afero.WriteFile(cs.fs, path, []byte(initial), 0o644))

	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {RateLimit: 500},
	}
	ctx, err := BuildSparseSaveContextWithScrapers([]string{"r18dev"}, scraperDefaultsForTest())
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	got, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	assert.NotContains(t, string(got), "cookies:", "empty cookies map must be fully removed from disk")
	assert.NotContains(t, string(got), "drop-me", "removed scraper cookie must be deleted from disk")
	assert.Contains(t, string(got), "rate_limit: 500", "unrelated scraper setting must remain")
}

func TestReconcileSparse_ScraperCookieUpdatedValueReplaced(t *testing.T) {
	cs := newMemStorage()
	path := "/cfg/config.yaml"
	initial := strings.Join([]string{
		"config_version: 3",
		"scrapers:",
		"    r18dev:",
		"        cookies:",
		"            session: old-value",
	}, "\n")
	require.NoError(t, afero.WriteFile(cs.fs, path, []byte(initial), 0o644))

	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Cookies: map[string]string{"session": "new-value"}},
	}
	ctx, err := BuildSparseSaveContextWithScrapers([]string{"r18dev"}, scraperDefaultsForTest())
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	got, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	assert.Contains(t, string(got), "new-value", "updated scraper cookie value must be persisted")
	assert.NotContains(t, string(got), "old-value", "stale scraper cookie value must be replaced")
}

func TestReconcileSparse_UnknownKeysStillPreservedWithCookieDeletion(t *testing.T) {
	cs := newMemStorage()
	path := "/cfg/config.yaml"
	initial := strings.Join([]string{
		"config_version: 2",
		"my_custom_key: custom_value",
		"scrapers:",
		"    r18dev:",
		"        cookies:",
		"            session: keep-me",
		"            stale: drop-me",
	}, "\n")
	require.NoError(t, afero.WriteFile(cs.fs, path, []byte(initial), 0o644))

	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Cookies: map[string]string{"session": "keep-me"}},
	}
	ctx, err := BuildSparseSaveContextWithScrapers([]string{"r18dev"}, scraperDefaultsForTest())
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	got, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	assert.Contains(t, string(got), "my_custom_key: custom_value", "unrelated unknown top-level key must be preserved")
	assert.Contains(t, string(got), "keep-me", "retained scraper cookie must remain")
	assert.NotContains(t, string(got), "drop-me", "removed scraper cookie must still be deleted")
}

func TestReconcileSparse_RemovedScraperCookieDeletedForMultipleScrapers(t *testing.T) {
	cs := newMemStorage()
	path := "/cfg/config.yaml"
	initial := strings.Join([]string{
		"config_version: 3",
		"scrapers:",
		"    r18dev:",
		"        cookies:",
		"            session: r18-keep",
		"            stale: r18-drop",
		"    dmm:",
		"        cookies:",
		"            token: dmm-keep",
		"            legacy: dmm-drop",
	}, "\n")
	require.NoError(t, afero.WriteFile(cs.fs, path, []byte(initial), 0o644))

	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Cookies: map[string]string{"session": "r18-keep"}},
		"dmm":    {Cookies: map[string]string{"token": "dmm-keep"}},
	}
	ctx, err := BuildSparseSaveContextWithScrapers([]string{"r18dev", "dmm"}, scraperDefaultsForTest())
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	got, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	assert.Contains(t, string(got), "r18-keep", "retained r18dev cookie must remain")
	assert.Contains(t, string(got), "dmm-keep", "retained dmm cookie must remain")
	assert.NotContains(t, string(got), "r18-drop", "removed r18dev cookie must be deleted")
	assert.NotContains(t, string(got), "dmm-drop", "removed dmm cookie must be deleted")
}
