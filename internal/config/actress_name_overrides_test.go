package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperconfig"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestActressNameOverridesLoadSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`scrapers:
  javlibrary:
    enabled: true
    actress_name_overrides:
      aa6sc:
        source_name: Yorimoto Shiori
        first_name: Shiori
        last_name: Yorimoto
`), 0600))
	cfg, err := Load(path)
	require.NoError(t, err)
	want := cfg.Scrapers.ResolvedSettings("javlibrary").ActressNameOverrides
	require.Len(t, want, 1)
	cfg.Scrapers.Overrides["javlibrary"].ActressNameOverrides["jnka"] = scraperconfig.ActressNameOverride{SourceName: "Jenna Jameson", FirstName: "Jenna", LastName: "Jameson"}
	want = cfg.Scrapers.ResolvedSettings("javlibrary").ActressNameOverrides
	// Unrelated configuration saves must not discard the opt-in map.
	cfg.Logging.Level = "debug"
	require.NoError(t, Save(cfg, path))
	reloaded, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, want, reloaded.Scrapers.ResolvedSettings("javlibrary").ActressNameOverrides)
	delete(reloaded.Scrapers.Overrides["javlibrary"].ActressNameOverrides, "jnka")
	require.NoError(t, Save(reloaded, path))
	partial, err := Load(path)
	require.NoError(t, err)
	require.Len(t, partial.Scrapers.ResolvedSettings("javlibrary").ActressNameOverrides, 1)
	require.Contains(t, partial.Scrapers.ResolvedSettings("javlibrary").ActressNameOverrides, "aa6sc")
	reloaded.Scrapers.Overrides["javlibrary"].ActressNameOverrides = nil
	require.NoError(t, Save(reloaded, path))
	cleared, err := Load(path)
	require.NoError(t, err)
	require.Empty(t, cleared.Scrapers.ResolvedSettings("javlibrary").ActressNameOverrides)
}

func TestActressNameOverridesSparseSave(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	settings := &models.ScraperSettings{Enabled: true, ActressNameOverrides: scraperconfig.ActressNameOverrides{
		"aa6sc": {SourceName: "Yorimoto Shiori", FirstName: "Shiori", LastName: "Yorimoto"},
		"jnka":  {SourceName: "Jenna Jameson", FirstName: "Jenna", LastName: "Jameson"},
	}}
	cfg.Scrapers.Overrides["javlibrary"] = settings
	cs := newMemStorage()
	ctx, err := BuildSparseSaveContextWithScrapers([]string{"javlibrary"}, map[string]models.ScraperSettings{"javlibrary": {Enabled: false}})
	require.NoError(t, err)
	path := "/cfg/override.yaml"
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))
	persisted := readPersistedScraperYAML(t, cs, path)
	require.Contains(t, persisted, "actress_name_overrides:")
	var decoded Config
	require.NoError(t, yaml.Unmarshal([]byte(persisted), &decoded))
	require.Equal(t, settings.ActressNameOverrides, decoded.Scrapers.Overrides["javlibrary"].ActressNameOverrides)
	cfg.Logging.Level = "debug"
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))
	require.Contains(t, readPersistedScraperYAML(t, cs, path), "source_name: Yorimoto Shiori")
	delete(cfg.Scrapers.Overrides["javlibrary"].ActressNameOverrides, "jnka")
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))
	partial := readPersistedScraperYAML(t, cs, path)
	require.Contains(t, partial, "aa6sc:")
	require.NotContains(t, partial, "jnka:", "removed IDs must not survive as unknown YAML keys")
	require.True(t, isSourceAuthoritativeFreeFormMap("scrapers.javlibrary.actress_name_overrides"))
	require.False(t, isSourceAuthoritativeFreeFormMap("scrapers.dmm.actress_name_overrides"))
	cfg.Scrapers.Overrides["javlibrary"].ActressNameOverrides = nil
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))
	require.NotContains(t, readPersistedScraperYAML(t, cs, path), "actress_name_overrides")
}

func TestReconcileJavLibraryNameOverridesMissingDestinations(t *testing.T) {
	for _, document := range []string{"{}", "scrapers: {javlibrary: null}", "scrapers: {javlibrary: {language: en}}"} {
		doc, err := parseYAMLDocument([]byte(document))
		require.NoError(t, err)
		before, err := yaml.Marshal(doc)
		require.NoError(t, err)
		reconcileJavLibraryNameOverrides(doc, nil)
		after, err := yaml.Marshal(doc)
		require.NoError(t, err)
		require.Equal(t, before, after)
	}
}
