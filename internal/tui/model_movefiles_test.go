package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/organizer"
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

// TestValidateMoveLinkMode verifies move mode and link mode are mutually exclusive,
// regardless of whether move mode came from config or the --move flag (issue #36).
func TestValidateMoveLinkMode(t *testing.T) {
	tests := []struct {
		name      string
		effective bool
		linkMode  organizer.LinkMode
		wantErr   bool
	}{
		{"move on, link none -> ok", true, organizer.LinkModeNone, false},
		{"move off, link hard -> ok", false, organizer.LinkModeHard, false},
		{"move off, link soft -> ok", false, organizer.LinkModeSoft, false},
		{"move on, link hard -> error", true, organizer.LinkModeHard, true},
		{"move on, link soft -> error", true, organizer.LinkModeSoft, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMoveLinkMode(tc.effective, tc.linkMode)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
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
	// --extrafanart, --scraper-priority, and the TUI-mode logging.output rewrite.
	loaded, err := config.Load(configPath)
	require.NoError(t, err)
	loaded.Output.DownloadExtrafanart = true // --extrafanart override
	loaded.Logging.Output = "data/logs/javinizer-tui.log"
	loaded.Scrapers.Priority = []string{"custom-scraper"} // --scraper-priority override

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
	assert.NotContains(t, reloaded.Scrapers.Priority, "custom-scraper",
		"--scraper-priority session override must not be persisted to config")
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
// when the on-disk config cannot be reloaded, and leaves the file untouched.
func TestSaveConfigNoOpWhenFileUnreadable(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Write invalid YAML so config.Update's internal load fails
	invalid := []byte("output: [invalid\n  not: valid")
	require.NoError(t, os.WriteFile(configPath, invalid, 0o644))

	cfg := config.DefaultConfig()
	m := New(cfg)
	m.SetConfigPath(configPath)
	m.moveFiles = true
	assert.NotPanics(t, func() { m.saveConfig() })

	// The corrupt file must be left as-is (no partial write)
	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, string(invalid), string(content), "corrupt config must not be rewritten")
}

// TestHandleSettingsKeys_ToggleMoveFiles verifies the real TUI keypress path:
// a space at settings cursor 3 toggles moveFiles, syncs the processor, and persists.
func TestHandleSettingsKeys_ToggleMoveFiles(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	cfg := config.DefaultConfig()
	cfg.Output.MoveFiles = false
	require.NoError(t, config.Save(cfg, configPath))

	loaded, err := config.Load(configPath)
	require.NoError(t, err)
	m := New(loaded)
	m.SetConfigPath(configPath)
	m.currentView = ViewSettings
	m.settingsCursor = 3 // "Move Files" row

	// Attach a processor to verify the toggle propagates
	proc := NewProcessingCoordinator(nil, nil, nil, nil, nil, nil, nil, nil, "", false)
	m.SetProcessor(proc)
	require.False(t, m.moveFiles)
	require.False(t, proc.moveFiles)

	// Simulate pressing space at the Move Files row
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m2 := updated.(*Model)

	assert.True(t, m2.moveFiles, "space at cursor 3 should toggle moveFiles on")
	assert.True(t, proc.moveFiles, "processor should be synced")

	reloaded, err := config.Load(configPath)
	require.NoError(t, err)
	assert.True(t, reloaded.Output.MoveFiles, "toggle should persist move_files")

	// Toggle back off through the same keypress path (true -> false)
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m3 := updated2.(*Model)
	assert.False(t, m3.moveFiles, "second space should toggle moveFiles off")
	assert.False(t, proc.moveFiles, "processor should sync back to false")
	reloaded2, err := config.Load(configPath)
	require.NoError(t, err)
	assert.False(t, reloaded2.Output.MoveFiles, "toggle-off should persist")
}
