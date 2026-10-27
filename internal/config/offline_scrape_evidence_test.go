package config

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOfflineScrapeEvidenceDefaultsAndIndependence(t *testing.T) {
	want := OfflineScrapeEvidenceConfig{MaxAgeHours: 168}
	require.Equal(t, want, DefaultConfig(nil, nil).Metadata.OfflineScrapeEvidence)
	for _, data := range []string{"config_version: 3\n", "metadata:\n  offline_scrape_evidence:\n    enabled: false\n"} {
		cfg, err := decodeConfig([]byte(data))
		require.NoError(t, err)
		require.Equal(t, want, cfg.Metadata.OfflineScrapeEvidence)
	}
	for _, path := range []string{"config.yaml.example", filepath.Join("..", "..", "configs", "config.yaml.example")} {
		cfg, err := Load(path)
		require.NoError(t, err)
		require.Equal(t, want, cfg.Metadata.OfflineScrapeEvidence)
	}
	cfg := DefaultConfig(nil, nil)
	cfg.Metadata.OfflineScrapeEvidence.Enabled = true
	cfg.Metadata.OfflineScrapeEvidence.ArtifactDirectory = filepath.Join(t.TempDir(), "not-provisioned")
	cfg.Metadata.ActressDatabase.Enabled = false
	require.NoError(t, ValidateConfig(cfg), "read capability needs no actress mutation service or I/O")
	require.False(t, cfg.Metadata.ActressDatabase.OfflineNameSync.Enabled)
	cfg.Metadata.OfflineScrapeEvidence = OfflineScrapeEvidenceConfig{}
	require.NoError(t, ValidateConfig(cfg))
	require.Equal(t, OfflineScrapeEvidenceConfig{}, cfg.Metadata.OfflineScrapeEvidence)
	require.Equal(t, want, cfg.Metadata.OfflineScrapeEvidence.Effective())
}

func TestOfflineScrapeEvidencePolicyValidation(t *testing.T) {
	for _, age := range []float64{0, -1, 0.5, 8760.1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		cfg := DefaultConfig(nil, nil)
		cfg.Metadata.OfflineScrapeEvidence.MaxAgeHours = age
		// Explicit zero disabled struct is legacy absence; a path distinguishes
		// partial configuration and preserves zero-value rejection.
		cfg.Metadata.OfflineScrapeEvidence.ArtifactDirectory = "unused"
		require.ErrorContains(t, ValidateConfig(cfg), "offline_scrape_evidence.max_age_hours")
		require.ErrorContains(t, validateConfigExcludingTranslationCredentials(cfg), "offline_scrape_evidence.max_age_hours")
	}
	for _, path := range []string{"", " ", "relative", ".", t.TempDir() + string(filepath.Separator), t.TempDir() + "\x00"} {
		cfg := defaultOfflineScrapeEvidenceConfig()
		cfg.Enabled, cfg.ArtifactDirectory = true, path
		require.ErrorContains(t, ValidateOfflineScrapeEvidence(cfg), "artifact_directory")
	}
	for _, age := range []float64{1, 1.5, 168, 8760} {
		require.NoError(t, ValidateOfflineScrapeEvidence(OfflineScrapeEvidenceConfig{MaxAgeHours: age}))
	}
	for _, value := range []string{".nan", ".inf", "-.inf"} {
		cfg, err := decodeConfig([]byte("metadata:\n  offline_scrape_evidence:\n    max_age_hours: " + value + "\n"))
		require.NoError(t, err)
		require.ErrorContains(t, ValidateConfig(cfg), "offline_scrape_evidence.max_age_hours")
	}
}
