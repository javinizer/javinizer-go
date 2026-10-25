package config

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
)

// OfflineNameSyncConfig controls explicitly confirmed offline Japanese-name
// jobs. All settings and the local artifact snapshot are pinned at API startup;
// saving or hot-reloading configuration does not change the running service.
type OfflineNameSyncConfig struct {
	Enabled           bool    `yaml:"enabled" json:"enabled"`
	ArtifactDirectory string  `yaml:"artifact_directory" json:"artifact_directory"`
	MaxAgeHours       float64 `yaml:"max_age_hours" json:"max_age_hours"`
	MaxQueuedJobs     int     `yaml:"max_queued_jobs" json:"max_queued_jobs"`
	MaxQueuedTasks    int     `yaml:"max_queued_tasks" json:"max_queued_tasks"`
}

func defaultOfflineNameSyncConfig() OfflineNameSyncConfig {
	return OfflineNameSyncConfig{
		Enabled:        false,
		MaxAgeHours:    168,
		MaxQueuedJobs:  16,
		MaxQueuedTasks: 2000,
	}
}

// Effective supplies the disabled defaults only for an entirely absent legacy
// programmatic config. Partial settings remain unchanged and validate strictly.
// YAML overlays defaults, so an individual zero is rejected; an entirely zero
// disabled object is indistinguishable from legacy absence and receives defaults.
// This fallback can never enable the service.
func (s OfflineNameSyncConfig) Effective() OfflineNameSyncConfig {
	if s == (OfflineNameSyncConfig{}) {
		return defaultOfflineNameSyncConfig()
	}
	return s
}

// ValidateOfflineNameSync checks bounds without accessing the artifact directory.
// Disabled services still require valid policy bounds, but need no configured
// artifact or enabled actress database to expose durable job history/cancel.
func ValidateOfflineNameSync(c ActressDatabaseConfig) error {
	const prefix = "metadata.actress_database.offline_name_sync."
	s := c.OfflineNameSync.Effective()
	if math.IsNaN(s.MaxAgeHours) || math.IsInf(s.MaxAgeHours, 0) || s.MaxAgeHours < 1 || s.MaxAgeHours > 8760 {
		return fmt.Errorf(prefix + "max_age_hours must be finite and between 1 and 8760")
	}
	if s.MaxQueuedJobs < 1 || s.MaxQueuedJobs > 32 {
		return fmt.Errorf(prefix + "max_queued_jobs must be between 1 and 32")
	}
	if s.MaxQueuedTasks < 1 || s.MaxQueuedTasks > 8192 {
		return fmt.Errorf(prefix + "max_queued_tasks must be between 1 and 8192")
	}
	if !s.Enabled {
		return nil
	}
	if !c.Enabled {
		return fmt.Errorf(prefix + "enabled requires metadata.actress_database.enabled")
	}
	dir := s.ArtifactDirectory
	if strings.TrimSpace(dir) == "" || strings.IndexByte(dir, 0) >= 0 || !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return fmt.Errorf(prefix + "artifact_directory must be a nonempty, clean absolute path when enabled")
	}
	return nil
}
