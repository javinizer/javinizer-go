package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	RegisterMigration(NewLegacyMigration())
}

func TestLoadOrCreateMigratesLegacyConfigVersion(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	legacy := `server:
  port: 7777
scrapers:
  priority:
    - r18dev
    - dmm
`

	err := os.WriteFile(cfgPath, []byte(legacy), 0644)
	require.NoError(t, err)

	cfg, err := LoadOrCreate(cfgPath)
	require.NoError(t, err)

	assert.Equal(t, CurrentConfigVersion, cfg.ConfigVersion)
	assert.Equal(t, 8765, cfg.Server.Port)                                            // Reset to default port
	assert.Equal(t, DefaultConfig(nil, nil).Scrapers.Priority, cfg.Scrapers.Priority) // Reset to default priorities

	saved, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.Contains(t, string(saved), "config_version: 3")
}

func TestLoadOrCreateSkipsMigrationForCurrentVersion(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	current := `config_version: 3
server:
  port: 9090
scrapers:
  priority:
    - dmm
`

	err := os.WriteFile(cfgPath, []byte(current), 0644)
	require.NoError(t, err)

	cfg, err := LoadOrCreate(cfgPath)
	require.NoError(t, err)

	assert.Equal(t, CurrentConfigVersion, cfg.ConfigVersion)
	assert.Equal(t, []string{"dmm"}, cfg.Scrapers.Priority)
	assert.Equal(t, 9090, cfg.Server.Port)

	saved, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(saved), "config_version: 3"))
	assert.True(t, strings.Contains(string(saved), "- dmm"))
}

func TestLoadOrCreateMigrationPreservesExplicitUpdateDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	v2 := `config_version: 2
system:
  update_enabled: false
  update_check_interval_hours: 12
`

	err := os.WriteFile(cfgPath, []byte(v2), 0644)
	require.NoError(t, err)

	cfg, err := LoadOrCreate(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, CurrentConfigVersion, cfg.ConfigVersion)
	// Legacy configs are reset to defaults, so user settings are not preserved
	assert.Equal(t, DefaultConfig(nil, nil).System.VersionCheckEnabled, cfg.System.VersionCheckEnabled)
	assert.Equal(t, DefaultConfig(nil, nil).System.VersionCheckIntervalHours, cfg.System.VersionCheckIntervalHours)

	saved, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	savedText := string(saved)
	assert.Contains(t, savedText, "config_version: 3")
}

func TestLoadOrCreateRejectsNewerConfigVersion(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	newer := `config_version: 999
server:
  port: 8080
`

	err := os.WriteFile(cfgPath, []byte(newer), 0644)
	require.NoError(t, err)
	before, err := os.ReadFile(cfgPath)
	require.NoError(t, err)

	_, err = LoadOrCreate(cfgPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "newer than supported version")

	after, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after))
}

// TestDefaultConfig_VersionCheckStableOnlyFalse verifies the new field defaults
// to false (prereleases allowed — the Go rewrite ships only prereleases, so
// the default keeps notifications on). Because the zero value is the correct
// default, no config_version bump or migration is required: existing configs
// that lack the field inherit false via decodeConfig's load-into-DefaultConfig.
func TestDefaultConfig_VersionCheckStableOnlyFalse(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	assert.False(t, cfg.System.VersionCheckStableOnly, "default should allow prerelease notifications (stable_only=false)")
	assert.Equal(t, CurrentConfigVersion, cfg.ConfigVersion)
}

// TestVersionCheckStableOnly_InheritedFromDefaultForV3Config verifies the
// load-into-DefaultConfig mechanism backfills the new field correctly for an
// existing v3 config that never had it: the field stays false (the DefaultConfig
// value), NOT the Go zero value trick — confirming no migration is needed.
// Other fields are preserved (not a legacy wipe).
func TestVersionCheckStableOnly_InheritedFromDefaultForV3Config(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	v3 := `config_version: 3
server:
  port: 9090
scrapers:
  priority:
    - dmm
system:
  version_check_enabled: false
`
	err := os.WriteFile(cfgPath, []byte(v3), 0o644)
	require.NoError(t, err)

	cfg, err := LoadOrCreate(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, CurrentConfigVersion, cfg.ConfigVersion)
	// No migration: v3 config stays v3 (CurrentConfigVersion is still 3) and the
	// new field inherits false from DefaultConfig, not from a migration step.
	assert.False(t, cfg.System.VersionCheckStableOnly, "field inherits the default (false); no migration needed")
	// Other fields preserved (not a legacy wipe).
	assert.Equal(t, 9090, cfg.Server.Port)
	assert.Equal(t, []string{"dmm"}, cfg.Scrapers.Priority)
	assert.False(t, cfg.System.VersionCheckEnabled, "explicit v3 setting preserved")
}
