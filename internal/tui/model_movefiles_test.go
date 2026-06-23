package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewLoadsMoveFilesFromConfig verifies the TUI initializes moveFiles from
// config.yaml (issue #36) instead of hardcoding it to false.
func TestNewLoadsMoveFilesFromConfig(t *testing.T) {
	t.Run("loads true from config", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Output.MoveFiles = true

		m := New(cfg)

		assert.True(t, m.moveFiles, "moveFiles should be loaded from cfg.Output.MoveFiles")
	})

	t.Run("loads false from config", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Output.MoveFiles = false

		m := New(cfg)

		assert.False(t, m.moveFiles, "moveFiles should be loaded from cfg.Output.MoveFiles")
	})
}

// TestSetMoveFilesSyncsModelAndProcessor verifies SetMoveFiles updates both the
// model state and (when present) the processor.
func TestSetMoveFilesSyncsModelAndProcessor(t *testing.T) {
	t.Run("without processor", func(t *testing.T) {
		cfg := config.DefaultConfig()
		m := New(cfg)

		m.SetMoveFiles(true)
		assert.True(t, m.moveFiles)

		m.SetMoveFiles(false)
		assert.False(t, m.moveFiles)
	})

	t.Run("with processor", func(t *testing.T) {
		cfg := config.DefaultConfig()
		m := New(cfg)
		proc := NewProcessingCoordinator(nil, nil, nil, nil, nil, nil, nil, nil, "", false)
		m.SetProcessor(proc)

		m.SetMoveFiles(true)
		assert.True(t, m.moveFiles)
		assert.True(t, proc.moveFiles, "SetMoveFiles should sync to the processor")

		m.SetMoveFiles(false)
		assert.False(t, proc.moveFiles)
	})
}

// TestResolveMoveMode verifies the --move flag overrides config, otherwise config wins.
func TestResolveMoveMode(t *testing.T) {
	tests := []struct {
		name            string
		configMoveFiles bool
		moveFlagSet     bool
		moveFlagValue   bool
		want            bool
	}{
		{"config true, no flag", true, false, false, true},
		{"config false, no flag", false, false, false, false},
		{"flag overrides config to true", false, true, true, true},
		{"flag overrides config to false", true, true, false, false},
		{"flag set, config true, flag false -> false", true, true, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ResolveMoveMode(tc.configMoveFiles, tc.moveFlagSet, tc.moveFlagValue))
		})
	}
}

// TestSaveConfigPersistsMoveFiles verifies toggling moveFiles and calling saveConfig
// writes move_files to config.yaml (reloaded and asserted via the parsed struct).
func TestSaveConfigPersistsMoveFiles(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := config.DefaultConfig()
	cfg.Output.MoveFiles = false
	require.NoError(t, config.Save(cfg, configPath))

	// Reload so the model starts from the persisted (false) state
	loaded, err := config.Load(configPath)
	require.NoError(t, err)
	require.False(t, loaded.Output.MoveFiles)

	m := New(loaded)
	m.SetConfigPath(configPath)

	// Simulate the TUI toggle: flip moveFiles on and persist
	m.moveFiles = true
	m.saveConfig()

	reloaded, err := config.Load(configPath)
	require.NoError(t, err)
	assert.True(t, reloaded.Output.MoveFiles, "move_files should be persisted as true after saveConfig")
}

// TestSaveConfigDoesNotLeakSessionOverrides verifies that saveConfig persists ONLY
// move_files and does not leak session-only CLI/env/TUI-mode overrides that were
// applied to the in-memory cfg (issue #36 review finding).
func TestSaveConfigDoesNotLeakSessionOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// On-disk config: extrafanart=false, logging output=stdout
	disk := config.DefaultConfig()
	disk.Output.MoveFiles = false
	disk.Output.DownloadExtrafanart = false
	disk.Logging.Output = "stdout"
	require.NoError(t, config.Save(disk, configPath))

	// In-memory cfg has session overrides applied (as the TUI command does):
	// --extrafanart, and the TUI-mode logging.output rewrite.
	loaded, err := config.Load(configPath)
	require.NoError(t, err)
	loaded.Output.DownloadExtrafanart = true // --extrafanart override
	loaded.Logging.Output = "data/logs/javinizer-tui.log"

	m := New(loaded)
	m.SetConfigPath(configPath)

	// Toggle moveFiles on and persist
	m.moveFiles = true
	m.saveConfig()

	reloaded, err := config.Load(configPath)
	require.NoError(t, err)

	// move_files SHOULD be persisted
	assert.True(t, reloaded.Output.MoveFiles, "move_files should be persisted")
	// Session overrides should NOT leak to disk
	assert.False(t, reloaded.Output.DownloadExtrafanart,
		"--extrafanart session override must not be persisted to config")
	assert.Equal(t, "stdout", reloaded.Logging.Output,
		"TUI-mode logging.output rewrite must not be persisted to config")
}

// TestSaveConfigEndToEndRestart verifies the full issue #36 scenario: a model
// toggles moveFiles on, persists, and a fresh model built from the reloaded
// config starts in move mode.
func TestSaveConfigEndToEndRestart(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := config.DefaultConfig()
	cfg.Output.MoveFiles = false
	require.NoError(t, config.Save(cfg, configPath))

	// Session 1: start in copy mode, toggle to move mode, persist
	loaded, _ := config.Load(configPath)
	m1 := New(loaded)
	m1.SetConfigPath(configPath)
	require.False(t, m1.moveFiles, "should start in copy mode")
	m1.moveFiles = true
	m1.saveConfig()

	// Session 2: reload config into a fresh model
	reloaded, err := config.Load(configPath)
	require.NoError(t, err)
	m2 := New(reloaded)
	assert.True(t, m2.moveFiles, "after restart the TUI should start in move mode (issue #36)")

	// Session 3: toggle back off and confirm it persists
	m2.SetConfigPath(configPath)
	m2.moveFiles = false
	m2.saveConfig()
	final, err := config.Load(configPath)
	require.NoError(t, err)
	assert.False(t, final.Output.MoveFiles, "toggling back off should persist")
}

// TestSaveConfigNoOpWithoutPath verifies saveConfig is a no-op when no config path
// is set (e.g. config not yet written), so it never crashes the TUI.
func TestSaveConfigNoOpWithoutPath(t *testing.T) {
	cfg := config.DefaultConfig()
	m := New(cfg)
	// configPath is empty by default
	m.moveFiles = true
	assert.NotPanics(t, func() { m.saveConfig() })
}

// TestSaveConfigNoOpWhenFileUnreadable verifies saveConfig logs and does not panic
// when the on-disk config cannot be reloaded.
func TestSaveConfigNoOpWhenFileUnreadable(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Write invalid YAML so config.Load fails
	require.NoError(t, os.WriteFile(configPath, []byte("output: [invalid\n  not: valid"), 0o644))

	cfg := config.DefaultConfig()
	m := New(cfg)
	m.SetConfigPath(configPath)
	m.moveFiles = true
	assert.NotPanics(t, func() { m.saveConfig() })
}
