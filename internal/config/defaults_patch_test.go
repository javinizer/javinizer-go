package config

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyDefaultsPatches(t *testing.T) {
	t.Run("nil config is a no-op", func(t *testing.T) {
		assert.False(t, applyDefaultsPatches(nil))
	})

	t.Run("negative version is not patched", func(t *testing.T) {
		cfg := &Config{DefaultsVersion: -1}
		assert.False(t, applyDefaultsPatches(cfg))
		assert.Equal(t, -1, cfg.DefaultsVersion)
	})

	t.Run("v0 with old default 60 becomes 180", func(t *testing.T) {
		cfg := &Config{DefaultsVersion: 0, Scrapers: ScrapersConfig{RequestTimeoutSeconds: 60}}
		assert.True(t, applyDefaultsPatches(cfg))
		assert.Equal(t, 180, cfg.Scrapers.RequestTimeoutSeconds)
		assert.Equal(t, 1, cfg.DefaultsVersion)
	})

	t.Run("v0 with non-default value is preserved but marker advances", func(t *testing.T) {
		cfg := &Config{DefaultsVersion: 0, Scrapers: ScrapersConfig{RequestTimeoutSeconds: 120}}
		assert.True(t, applyDefaultsPatches(cfg))
		assert.Equal(t, 120, cfg.Scrapers.RequestTimeoutSeconds)
		assert.Equal(t, 1, cfg.DefaultsVersion)
	})

	t.Run("already at current version is a no-op even with 60", func(t *testing.T) {
		cfg := &Config{DefaultsVersion: 1, Scrapers: ScrapersConfig{RequestTimeoutSeconds: 60}}
		assert.False(t, applyDefaultsPatches(cfg))
		assert.Equal(t, 60, cfg.Scrapers.RequestTimeoutSeconds)
		assert.Equal(t, 1, cfg.DefaultsVersion)
	})

	t.Run("future version is not patched", func(t *testing.T) {
		cfg := &Config{DefaultsVersion: CurrentDefaultsVersion + 1, Scrapers: ScrapersConfig{RequestTimeoutSeconds: 60}}
		assert.False(t, applyDefaultsPatches(cfg))
		assert.Equal(t, CurrentDefaultsVersion+1, cfg.DefaultsVersion)
		assert.Equal(t, 60, cfg.Scrapers.RequestTimeoutSeconds)
	})
}

func TestDefaultsVersion_LoadOrCreatePatching(t *testing.T) {
	tests := []struct {
		name             string
		yaml             string
		wantTimeout      int
		wantDefaultsVer  int
		wantSavedVersion int
	}{
		{
			name: "v3 config with old default 60 and no defaults_version key",
			yaml: `config_version: 3
scrapers:
    request_timeout_seconds: 60
`,
			wantTimeout:      180,
			wantDefaultsVer:  1,
			wantSavedVersion: 1,
		},
		{
			name: "v3 config with user value 120 and defaults_version 0",
			yaml: `config_version: 3
defaults_version: 0
scrapers:
    request_timeout_seconds: 120
`,
			wantTimeout:      120,
			wantDefaultsVer:  1,
			wantSavedVersion: 1,
		},
		{
			name: "already patched config keeps deliberate 60",
			yaml: `config_version: 3
defaults_version: 1
scrapers:
    request_timeout_seconds: 60
`,
			wantTimeout:      60,
			wantDefaultsVer:  1,
			wantSavedVersion: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")
			require.NoError(t, os.WriteFile(configPath, []byte(tc.yaml), 0644))

			cfg, err := LoadOrCreate(configPath)
			require.NoError(t, err)
			assert.Equal(t, tc.wantTimeout, cfg.Scrapers.RequestTimeoutSeconds)
			assert.Equal(t, tc.wantDefaultsVer, cfg.DefaultsVersion)

			saved, err := os.ReadFile(configPath)
			require.NoError(t, err)
			assert.Contains(t, string(saved), "defaults_version: 1")
		})
	}
}

func TestDefaultsVersion_Validation(t *testing.T) {
	t.Run("future defaults version rejected", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.DefaultsVersion = CurrentDefaultsVersion + 1
		_, err := Prepare(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "newer than supported")
	})

	t.Run("negative defaults version rejected", func(t *testing.T) {
		cfg := DefaultConfig(nil, nil)
		cfg.DefaultsVersion = -1
		_, err := Prepare(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be >= 0")
	})
}

func TestFreshConfigFromEmbeddedHasCurrentDefaultsVersion(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg, err := LoadOrCreate(configPath)
	require.NoError(t, err)
	assert.Equal(t, CurrentDefaultsVersion, cfg.DefaultsVersion)
	assert.Equal(t, 180, cfg.Scrapers.RequestTimeoutSeconds)
}

func TestEmbeddedConfigCarriesCurrentDefaultsVersion(t *testing.T) {
	content := string(embeddedConfigBytes())
	assert.Contains(t, content, "defaults_version: 1")

	cfg, err := decodeConfig(embeddedConfigBytes())
	require.NoError(t, err)
	assert.Equal(t, CurrentDefaultsVersion, cfg.DefaultsVersion)
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	defer func() { os.Stderr = old }()

	fn()

	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
}

func TestApplyDefaultsPatches_Notice(t *testing.T) {
	t.Run("prints notice when a value is rewritten", func(t *testing.T) {
		cfg := &Config{DefaultsVersion: 0, Scrapers: ScrapersConfig{RequestTimeoutSeconds: 60}}
		out := captureStderr(t, func() { applyDefaultsPatches(cfg) })
		assert.Contains(t, out, "scrapers.request_timeout_seconds")
		assert.Contains(t, out, "180")
	})

	t.Run("silent when only the marker advances", func(t *testing.T) {
		cfg := &Config{DefaultsVersion: 0, Scrapers: ScrapersConfig{RequestTimeoutSeconds: 120}}
		out := captureStderr(t, func() { applyDefaultsPatches(cfg) })
		assert.Empty(t, out)
	})
}

// TestLoadOrCreate_DoesNotPersistEnvOverridesDuringDefaultsPatch guards the
// P1 regression: when the defaults patch triggers a save, environment
// overrides (which may carry secrets like OPENAI_API_KEY or paths like
// JAVINIZER_DB) must not be written to config.yaml. The runtime cfg still
// honors env overrides; only the on-disk copy excludes them.
func TestLoadOrCreate_DoesNotPersistEnvOverridesDuringDefaultsPatch(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("config_version: 3\nscrapers:\n    request_timeout_seconds: 60\n"), 0644))

	t.Setenv("JAVINIZER_DB", "/env/secret/db.sqlite")
	t.Setenv("OPENAI_API_KEY", "sk-env-secret-do-not-persist")

	cfg, err := LoadOrCreate(configPath)
	require.NoError(t, err)

	// Runtime cfg honors env overrides + the defaults patch.
	assert.Equal(t, "/env/secret/db.sqlite", cfg.Database.DSN, "runtime cfg honors JAVINIZER_DB")
	assert.Equal(t, "sk-env-secret-do-not-persist", cfg.Metadata.Translation.OpenAI.APIKey, "runtime cfg honors OPENAI_API_KEY")
	assert.Equal(t, 180, cfg.Scrapers.RequestTimeoutSeconds)
	assert.Equal(t, CurrentDefaultsVersion, cfg.DefaultsVersion)

	// Persisted file carries the patch but never env overrides (esp. secrets).
	saved, err := os.ReadFile(configPath)
	require.NoError(t, err)
	savedStr := string(saved)
	assert.Contains(t, savedStr, "defaults_version: 1")
	assert.Contains(t, savedStr, "request_timeout_seconds: 180")
	assert.NotContains(t, savedStr, "/env/secret/db.sqlite", "env JAVINIZER_DB must not be persisted to config.yaml")
	assert.NotContains(t, savedStr, "sk-env-secret-do-not-persist", "env OPENAI_API_KEY must not be persisted to config.yaml")
}
