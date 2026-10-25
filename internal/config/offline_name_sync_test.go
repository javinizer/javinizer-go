package config

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOfflineNameSyncDefaultsAndDecode(t *testing.T) {
	want := OfflineNameSyncConfig{MaxAgeHours: 168, MaxQueuedJobs: 16, MaxQueuedTasks: 2000}
	require.Equal(t, want, DefaultConfig(nil, nil).Metadata.ActressDatabase.OfflineNameSync)
	for _, data := range []string{"config_version: 3\n", "metadata:\n  actress_database:\n    offline_name_sync:\n      enabled: false\n"} {
		cfg, err := decodeConfig([]byte(data))
		require.NoError(t, err)
		require.Equal(t, want, cfg.Metadata.ActressDatabase.OfflineNameSync)
	}
	for _, path := range []string{"config.yaml.example", filepath.Join("..", "..", "configs", "config.yaml.example")} {
		cfg, err := Load(path)
		require.NoError(t, err)
		require.Equal(t, want, cfg.Metadata.ActressDatabase.OfflineNameSync)
	}
}

func TestOfflineNameSyncAbsentLegacyProgrammaticSettings(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Metadata.ActressDatabase.OfflineNameSync = OfflineNameSyncConfig{}
	require.NoError(t, ValidateConfig(cfg))
	require.NoError(t, validateConfigExcludingTranslationCredentials(cfg))
	require.Equal(t, OfflineNameSyncConfig{}, cfg.Metadata.ActressDatabase.OfflineNameSync, "validation must remain nonmutating")
	require.Equal(t, defaultOfflineNameSyncConfig(), cfg.Metadata.ActressDatabase.OfflineNameSync.Effective())
	cfg.Metadata.ActressDatabase.OfflineNameSync.Enabled = true
	require.ErrorContains(t, ValidateConfig(cfg), "offline_name_sync.max_age_hours", "enabled partial settings must never silently default")
}

func TestOfflineNameSyncValidationBounds(t *testing.T) {
	tests := []struct {
		name  string
		field string
		edit  func(*OfflineNameSyncConfig)
	}{
		{"zero age", "max_age_hours", func(c *OfflineNameSyncConfig) { c.MaxAgeHours = 0 }},
		{"negative age", "max_age_hours", func(c *OfflineNameSyncConfig) { c.MaxAgeHours = -1 }},
		{"too fresh", "max_age_hours", func(c *OfflineNameSyncConfig) { c.MaxAgeHours = 0.5 }},
		{"too old", "max_age_hours", func(c *OfflineNameSyncConfig) { c.MaxAgeHours = 8760.01 }},
		{"NaN age", "max_age_hours", func(c *OfflineNameSyncConfig) { c.MaxAgeHours = math.NaN() }},
		{"positive infinite age", "max_age_hours", func(c *OfflineNameSyncConfig) { c.MaxAgeHours = math.Inf(1) }},
		{"negative infinite age", "max_age_hours", func(c *OfflineNameSyncConfig) { c.MaxAgeHours = math.Inf(-1) }},
		{"zero jobs", "max_queued_jobs", func(c *OfflineNameSyncConfig) { c.MaxQueuedJobs = 0 }},
		{"negative jobs", "max_queued_jobs", func(c *OfflineNameSyncConfig) { c.MaxQueuedJobs = -1 }},
		{"too many jobs", "max_queued_jobs", func(c *OfflineNameSyncConfig) { c.MaxQueuedJobs = 33 }},
		{"zero tasks", "max_queued_tasks", func(c *OfflineNameSyncConfig) { c.MaxQueuedTasks = 0 }},
		{"negative tasks", "max_queued_tasks", func(c *OfflineNameSyncConfig) { c.MaxQueuedTasks = -1 }},
		{"too many tasks", "max_queued_tasks", func(c *OfflineNameSyncConfig) { c.MaxQueuedTasks = 8193 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig(nil, nil)
			tt.edit(&cfg.Metadata.ActressDatabase.OfflineNameSync)
			// Both startup validation and secret-preserving config persistence
			// reject bad values, even while the feature is disabled.
			require.ErrorContains(t, ValidateConfig(cfg), "offline_name_sync."+tt.field)
			require.ErrorContains(t, validateConfigExcludingTranslationCredentials(cfg), "offline_name_sync."+tt.field)
		})
	}
	for _, age := range []float64{1, 1.5, 168, 8760} {
		for _, jobs := range []int{1, 32} {
			for _, tasks := range []int{1, 8192} {
				cfg := DefaultConfig(nil, nil).Metadata.ActressDatabase
				cfg.OfflineNameSync.MaxAgeHours = age
				cfg.OfflineNameSync.MaxQueuedJobs = jobs
				cfg.OfflineNameSync.MaxQueuedTasks = tasks
				require.NoError(t, ValidateOfflineNameSync(cfg))
			}
		}
	}
}

func TestOfflineNameSyncEnabledRequiresCleanAbsolutePathAndActorDatabase(t *testing.T) {
	cfg := DefaultConfig(nil, nil).Metadata.ActressDatabase
	cfg.OfflineNameSync.Enabled = true
	root := t.TempDir()
	cfg.OfflineNameSync.ArtifactDirectory = filepath.Join(root, "not-provisioned")
	require.NoError(t, ValidateOfflineNameSync(cfg), "validation must not do artifact I/O")
	for _, path := range []string{"", " ", "relative", ".", root + string(filepath.Separator), root + string(filepath.Separator) + "..", root + "\x00"} {
		cfg.OfflineNameSync.ArtifactDirectory = path
		require.ErrorContains(t, ValidateOfflineNameSync(cfg), "artifact_directory")
	}
	cfg.OfflineNameSync.ArtifactDirectory = root
	cfg.Enabled = false
	require.ErrorContains(t, ValidateOfflineNameSync(cfg), "requires metadata.actress_database.enabled")
	cfg.OfflineNameSync.Enabled = false
	cfg.OfflineNameSync.ArtifactDirectory = ""
	require.NoError(t, ValidateOfflineNameSync(cfg), "disabled mode does not need an actor database or artifact")
}

func TestOfflineNameSyncDecodePreservesExplicitInvalidValues(t *testing.T) {
	for _, value := range []string{"0", ".nan", ".inf", "-.inf"} {
		cfg, err := decodeConfig([]byte("metadata:\n  actress_database:\n    offline_name_sync:\n      max_age_hours: " + value + "\n"))
		require.NoError(t, err)
		require.ErrorContains(t, ValidateConfig(cfg), "offline_name_sync.max_age_hours")
	}
}
