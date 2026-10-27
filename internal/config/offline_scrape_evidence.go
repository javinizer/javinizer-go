package config

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
)

// OfflineScrapeEvidenceConfig enables read-only review, never name-sync consent.
// Directory and policy are administrator-owned and pinned at runtime startup.
type OfflineScrapeEvidenceConfig struct {
	Enabled           bool    `yaml:"enabled" json:"enabled"`
	ArtifactDirectory string  `yaml:"artifact_directory" json:"artifact_directory"`
	MaxAgeHours       float64 `yaml:"max_age_hours" json:"max_age_hours"`
}

func defaultOfflineScrapeEvidenceConfig() OfflineScrapeEvidenceConfig {
	return OfflineScrapeEvidenceConfig{MaxAgeHours: 168}
}

// Effective defaults only entirely absent legacy configurations, never enables.
func (c OfflineScrapeEvidenceConfig) Effective() OfflineScrapeEvidenceConfig {
	if c == (OfflineScrapeEvidenceConfig{}) {
		return defaultOfflineScrapeEvidenceConfig()
	}
	return c
}

// ValidateOfflineScrapeEvidence performs no filesystem I/O or mutation.
func ValidateOfflineScrapeEvidence(c OfflineScrapeEvidenceConfig) error {
	c = c.Effective()
	const prefix = "metadata.offline_scrape_evidence."
	if math.IsNaN(c.MaxAgeHours) || math.IsInf(c.MaxAgeHours, 0) || c.MaxAgeHours < 1 || c.MaxAgeHours > 8760 {
		return fmt.Errorf(prefix + "max_age_hours must be finite and between 1 and 8760")
	}
	if c.Enabled && (strings.TrimSpace(c.ArtifactDirectory) == "" || strings.IndexByte(c.ArtifactDirectory, 0) >= 0 || !filepath.IsAbs(c.ArtifactDirectory) || filepath.Clean(c.ArtifactDirectory) != c.ArtifactDirectory) {
		return fmt.Errorf(prefix + "artifact_directory must be a nonempty, clean absolute path when enabled")
	}
	return nil
}
