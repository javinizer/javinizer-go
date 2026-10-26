package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLegacyBackupReadFailurePreservesConfigAndContext(t *testing.T) {
	previous := GetMigrationContext()
	t.Cleanup(func() { SetMigrationContext(previous) })
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.Mkdir(path, 0700))
	sentinel := filepath.Join(path, "keep")
	require.NoError(t, os.WriteFile(sentinel, []byte("original"), 0600))
	ctx := MigrationContext{ConfigPath: path}
	SetMigrationContext(ctx)
	cfg := DefaultConfig(nil, nil)
	cfg.Logging.Level = "debug"
	before := cfg.Clone()
	err := (&LegacyMigration{}).Migrate(cfg)
	require.ErrorContains(t, err, "failed to read config for backup")
	require.Equal(t, before, cfg)
	require.Equal(t, ctx, GetMigrationContext())
	data, err := os.ReadFile(sentinel)
	require.NoError(t, err)
	require.Equal(t, "original", string(data))
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1, "no backup should be published")
}

func TestMalformedHTTPBaseURLRejectedBeforeScraperAllowlist(t *testing.T) {
	for _, raw := range []string{"https://%invalid", "https://host.invalid/%zz"} {
		require.ErrorContains(t, ValidateHTTPBaseURL("source.base_url", raw), "source.base_url must be a valid URL")
		require.ErrorContains(t, ValidateScraperBaseURL("source.base_url", raw, []string{"host.invalid"}), "source.base_url must be a valid URL")
	}
}

func TestDumpEnvironmentOverrideUsesInjectedLookup(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	path := filepath.Join(t.TempDir(), "offline-dump.json")
	ApplyEnvironmentOverrides(cfg, func(key string) string {
		if key == "JAVINIZER_R18DEV_DUMP_PATH" {
			return path
		}
		return ""
	})
	require.Equal(t, path, cfg.Metadata.R18DevDump.Path)
	require.NoFileExists(t, path, "configuration override must not create dump")
}

func TestAnthropicModelChangesTranslationCacheKey(t *testing.T) {
	cfg := TranslationConfig{Enabled: true, Provider: "anthropic", SourceLanguage: "ja", TargetLanguage: "en", Anthropic: AnthropicTranslationConfig{Model: "model-one"}}
	before := cfg.SettingsHash()
	require.Len(t, before, 16)
	cfg.Anthropic.Model = "model-two"
	require.NotEqual(t, before, cfg.SettingsHash())
	require.Equal(t, cfg.SettingsHash(), cfg.SettingsHash())
}
