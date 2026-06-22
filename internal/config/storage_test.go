package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadOrCreateUsesEmbeddedConfig verifies new configs are created from embedded example
func TestLoadOrCreateUsesEmbeddedConfig(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Ensure file doesn't exist
	_, err := os.Stat(configPath)
	assert.True(t, os.IsNotExist(err), "config file should not exist initially")

	// Call LoadOrCreate - should create from embedded
	cfg, err := LoadOrCreate(configPath)
	require.NoError(t, err, "LoadOrCreate should succeed")
	require.NotNil(t, cfg, "config should not be nil")

	// Verify file was created
	content, err := os.ReadFile(configPath)
	require.NoError(t, err, "should be able to read created config file")
	require.NotEmpty(t, content, "created config file should not be empty")

	// Verify config has expected values
	assert.Equal(t, CurrentConfigVersion, cfg.ConfigVersion, "config_version should match current")
	assert.NotEmpty(t, cfg.Server.Host, "server.host should be set")
	assert.Greater(t, cfg.Server.Port, 0, "server.port should be positive")

	// Verify file contains comments (indicates embedded example was used)
	contentStr := string(content)
	hasComments := strings.Contains(contentStr, "#")
	assert.True(t, hasComments, "created config should contain comments from example file")
}

// TestLoadOrCreatePreservesExistingConfig verifies existing configs are not modified
func TestLoadOrCreatePreservesExistingConfig(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create a custom config file with specific values
	customContent := `config_version: 3
server:
    host: custom-host
    port: 9999
scrapers:
    user_agent: CustomAgent
metadata:
    nfo:
        enabled: false
`
	err := os.WriteFile(configPath, []byte(customContent), 0644)
	require.NoError(t, err, "should be able to write test config")

	// Call LoadOrCreate
	cfg, err := LoadOrCreate(configPath)
	require.NoError(t, err, "LoadOrCreate should succeed")

	// Verify custom values are preserved
	assert.Equal(t, "custom-host", cfg.Server.Host, "custom host should be preserved")
	assert.Equal(t, 9999, cfg.Server.Port, "custom port should be preserved")
	assert.Equal(t, "CustomAgent", cfg.Scrapers.UserAgent, "custom user_agent should be preserved")
	assert.False(t, cfg.Metadata.NFO.Feature.Enabled, "custom nfo.enabled should be preserved")
}

// TestCreateConfigFromEmbedded creates a config and verifies structure
func TestCreateConfigFromEmbedded(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "new-config.yaml")

	cfg, err := createConfigFromEmbedded(configPath)
	require.NoError(t, err, "createConfigFromEmbedded should succeed")
	require.NotNil(t, cfg, "config should not be nil")

	// Verify file was created
	_, err = os.Stat(configPath)
	assert.NoError(t, err, "config file should exist after creation")

	// Verify key structure
	assert.Equal(t, CurrentConfigVersion, cfg.ConfigVersion, "config_version should match current")
	assert.NotEmpty(t, cfg.Scrapers.Priority, "scrapers.priority should be populated")
	assert.NotEmpty(t, cfg.Scrapers.Overrides, "scrapers.overrides should be populated from registry")
}

// TestLoadOrCreateIdempotent verifies calling LoadOrCreate twice produces same result
func TestLoadOrCreateIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// First call - creates file
	cfg1, err := LoadOrCreate(configPath)
	require.NoError(t, err)

	// Second call - loads existing
	cfg2, err := LoadOrCreate(configPath)
	require.NoError(t, err)

	// Configs should be equivalent
	assert.Equal(t, cfg1.ConfigVersion, cfg2.ConfigVersion, "config_version should match")
	assert.Equal(t, cfg1.Server.Host, cfg2.Server.Host, "server.host should match")
	assert.Equal(t, cfg1.Server.Port, cfg2.Server.Port, "server.port should match")
	assert.Equal(t, cfg1.Scrapers.UserAgent, cfg2.Scrapers.UserAgent, "user_agent should match")
}

// TestMoveFilesRoundTrip verifies the move_files setting survives a save/load cycle (issue #36).
func TestMoveFilesRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg, err := LoadOrCreate(configPath)
	require.NoError(t, err)

	// Enable move mode and persist
	cfg.Output.Operation.MoveFiles = true
	require.NoError(t, Save(cfg, configPath))

	// Reload and confirm the setting persisted
	reloaded, err := LoadOrCreate(configPath)
	require.NoError(t, err)
	assert.True(t, reloaded.Output.Operation.MoveFiles, "move_files should persist as true after save/reload")

	// Flip back to false and confirm that also persists
	reloaded.Output.Operation.MoveFiles = false
	require.NoError(t, Save(reloaded, configPath))
	final, err := LoadOrCreate(configPath)
	require.NoError(t, err)
	assert.False(t, final.Output.Operation.MoveFiles, "move_files should persist as false after save/reload")
}

// TestUpdateAtomicallyModifiesSingleField verifies config.Update performs an atomic
// read-modify-write that only changes the mutated field and preserves other values
// (issue #36: persisting move_files without leaking session overrides).
func TestUpdateAtomicallyModifiesSingleField(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig(nil, nil)
	cfg.Output.Operation.MoveFiles = false
	cfg.Output.Download.DownloadExtrafanart = false
	require.NoError(t, Save(cfg, configPath))

	// Use Update to persist only move_files (simulating a TUI toggle).
	require.NoError(t, Update(configPath, func(c *Config) {
		c.Output.Operation.MoveFiles = true
	}))

	reloaded, err := LoadOrCreate(configPath)
	require.NoError(t, err)
	assert.True(t, reloaded.Output.Operation.MoveFiles, "Update should persist move_files")
	assert.False(t, reloaded.Output.Download.DownloadExtrafanart, "Update must not touch unrelated fields")
}

// TestUpdateConcurrentWritersNoLostUpdates proves config.Update is an atomic
// read-modify-write: 100 concurrent increments of a counter field must all
// survive (no lost updates). With an unlocked Load+Save, many increments
// would be lost. This is the TOCTOU regression test for issue #36.
func TestUpdateConcurrentWritersNoLostUpdates(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrency test in short mode")
	}
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	cfg := DefaultConfig(nil, nil)
	cfg.Output.MediaFormat.MaxPosterHeight = 0
	require.NoError(t, Save(cfg, configPath))

	const N = 100
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = Update(configPath, func(c *Config) { c.Output.MediaFormat.MaxPosterHeight++ })
		}()
	}
	wg.Wait()

	reloaded, err := LoadOrCreate(configPath)
	require.NoError(t, err)
	assert.Equal(t, N, reloaded.Output.MediaFormat.MaxPosterHeight, "no concurrent updates should be lost (atomic RMW under lock)")
}

// TestUpdateOnMissingFileWritesDefaults verifies Update on a non-existent path
// writes a config with the mutation applied (loadLocked returns DefaultConfig(nil, nil)
// for missing files).
func TestUpdateOnMissingFileWritesDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	require.NoError(t, Update(configPath, func(c *Config) { c.Output.Operation.MoveFiles = true }))

	reloaded, err := LoadOrCreate(configPath)
	require.NoError(t, err)
	assert.True(t, reloaded.Output.Operation.MoveFiles, "Update on a missing file should write the mutation")
}

// TestUpdate_NilMutateReturnsError guards against a nil callback panicking inside
// the locked read-modify-write (CodeRabbit finding).
func TestUpdate_NilMutateReturnsError(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, Save(DefaultConfig(nil, nil), configPath))

	err := Update(configPath, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutate", "error should explain the nil callback")
}

// TestCreatedConfigHasComments verifies new configs preserve example comments
func TestCreatedConfigHasComments(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	_, err := LoadOrCreate(configPath)
	require.NoError(t, err)

	content, err := os.ReadFile(configPath)
	require.NoError(t, err)

	contentStr := string(content)

	// Check for specific comments that exist in the example file
	assert.Contains(t, contentStr, "#", "config should contain comment characters")
	assert.Contains(t, contentStr, "config_version:", "config should contain config_version")
	assert.Contains(t, contentStr, "server:", "config should contain server section")
	assert.Contains(t, contentStr, "scrapers:", "config should contain scrapers section")
}
