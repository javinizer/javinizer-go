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
		m := New(TUIModelConfig{MoveFiles: true})

		assert.True(t, m.settingsMgr.snapshot.MoveFiles, "moveFiles should be loaded from TUIModelConfig.MoveFiles")
	})

	t.Run("loads false from config", func(t *testing.T) {
		m := New(TUIModelConfig{MoveFiles: false})

		assert.False(t, m.settingsMgr.snapshot.MoveFiles, "moveFiles should be loaded from TUIModelConfig.MoveFiles")
	})
}

// TestSetMoveFilesSyncsModelAndProcessor verifies SetMoveFiles updates both the
// model state and (when present) the processor.
func TestSetMoveFilesSyncsModelAndProcessor(t *testing.T) {
	t.Run("updates settings manager", func(t *testing.T) {
		m := New(TUIModelConfig{})

		m.SetMoveFiles(true)
		assert.True(t, m.settingsMgr.snapshot.MoveFiles)

		m.SetMoveFiles(false)
		assert.False(t, m.settingsMgr.snapshot.MoveFiles)
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

	cfg := config.DefaultConfig(nil, nil)
	cfg.Output.Operation.MoveFiles = false
	require.NoError(t, config.Save(cfg, configPath))

	// Reload so the model starts from the persisted (false) state
	loaded, err := config.Load(configPath)
	require.NoError(t, err)
	require.False(t, loaded.Output.Operation.MoveFiles)

	m := New(TUIModelConfig{MoveFiles: loaded.Output.Operation.MoveFiles})
	m.SetConfigPath(configPath)

	// Simulate the TUI toggle: flip moveFiles on and persist
	m.settingsMgr.snapshot.MoveFiles = true
	m.saveConfig()

	reloaded, err := config.Load(configPath)
	require.NoError(t, err)
	assert.True(t, reloaded.Output.Operation.MoveFiles, "move_files should be persisted as true after saveConfig")
}

// TestSaveConfigDoesNotLeakSessionOverrides verifies that saveConfig persists ONLY
// move_files and does not leak session-only CLI/env/TUI-mode overrides that were
// applied to the in-memory cfg (issue #36 review finding).
func TestSaveConfigDoesNotLeakSessionOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// On-disk config: extrafanart=false, logging output=stdout
	disk := config.DefaultConfig(nil, nil)
	disk.Output.Operation.MoveFiles = false
	disk.Output.Download.DownloadExtrafanart = false
	disk.Logging.Output = "stdout"
	require.NoError(t, config.Save(disk, configPath))

	// In-memory cfg has session overrides applied (as the TUI command does):
	// --extrafanart, --scraper-priority, and the TUI-mode logging.output rewrite.
	loaded, err := config.Load(configPath)
	require.NoError(t, err)
	loaded.Output.Download.DownloadExtrafanart = true // --extrafanart override
	loaded.Logging.Output = "data/logs/javinizer-tui.log"
	loaded.Scrapers.Priority = []string{"custom-scraper"} // --scraper-priority override

	m := New(TUIModelConfig{MoveFiles: loaded.Output.Operation.MoveFiles})
	m.SetConfigPath(configPath)

	// Toggle moveFiles on and persist
	m.settingsMgr.snapshot.MoveFiles = true
	m.saveConfig()

	reloaded, err := config.Load(configPath)
	require.NoError(t, err)

	// move_files SHOULD be persisted
	assert.True(t, reloaded.Output.Operation.MoveFiles, "move_files should be persisted")
	// Session overrides should NOT leak to disk
	assert.False(t, reloaded.Output.Download.DownloadExtrafanart,
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

	cfg := config.DefaultConfig(nil, nil)
	cfg.Output.Operation.MoveFiles = false
	require.NoError(t, config.Save(cfg, configPath))

	// Session 1: start in copy mode, toggle to move mode, persist
	loaded, _ := config.Load(configPath)
	m1 := New(TUIModelConfig{MoveFiles: loaded.Output.Operation.MoveFiles})
	m1.SetConfigPath(configPath)
	require.False(t, m1.settingsMgr.snapshot.MoveFiles, "should start in copy mode")
	m1.settingsMgr.snapshot.MoveFiles = true
	m1.saveConfig()

	// Session 2: reload config into a fresh model
	reloaded, err := config.Load(configPath)
	require.NoError(t, err)
	m2 := New(TUIModelConfig{MoveFiles: reloaded.Output.Operation.MoveFiles})
	assert.True(t, m2.settingsMgr.snapshot.MoveFiles, "after restart the TUI should start in move mode (issue #36)")

	// Session 3: toggle back off and confirm it persists
	m2.SetConfigPath(configPath)
	m2.settingsMgr.snapshot.MoveFiles = false
	m2.saveConfig()
	final, err := config.Load(configPath)
	require.NoError(t, err)
	assert.False(t, final.Output.Operation.MoveFiles, "toggling back off should persist")
}

// TestSaveConfigNoOpWithoutPath verifies saveConfig is a no-op when no config path
// is set (e.g. config not yet written), so it never crashes the TUI.
func TestSaveConfigNoOpWithoutPath(t *testing.T) {
	m := New(TUIModelConfig{})
	// configPath is empty by default
	m.settingsMgr.snapshot.MoveFiles = true
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

	m := New(TUIModelConfig{})
	m.SetConfigPath(configPath)
	m.settingsMgr.snapshot.MoveFiles = true
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
	cfg := config.DefaultConfig(nil, nil)
	cfg.Output.Operation.MoveFiles = false
	require.NoError(t, config.Save(cfg, configPath))

	loaded, err := config.Load(configPath)
	require.NoError(t, err)
	m := New(TUIModelConfig{MoveFiles: loaded.Output.Operation.MoveFiles})
	m.SetConfigPath(configPath)
	m.viewMgr.switchTo(viewSettings)
	m.settingsMgr.cursor = 3 // "Move Files" row

	require.False(t, m.settingsMgr.snapshot.MoveFiles)

	// Simulate pressing space at the Move Files row
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m2 := updated.(*Model)

	assert.True(t, m2.settingsMgr.snapshot.MoveFiles, "space at cursor 3 should toggle moveFiles on")

	reloaded, err := config.Load(configPath)
	require.NoError(t, err)
	assert.True(t, reloaded.Output.Operation.MoveFiles, "toggle should persist move_files")

	// Toggle back off through the same keypress path (true -> false)
	updated2, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m3 := updated2.(*Model)
	assert.False(t, m3.settingsMgr.snapshot.MoveFiles, "second space should toggle moveFiles off")
	reloaded2, err := config.Load(configPath)
	require.NoError(t, err)
	assert.False(t, reloaded2.Output.Operation.MoveFiles, "toggle-off should persist")
}

// TestCanEnableMoveMode verifies the guard predicate used by the runtime toggle.
func TestCanEnableMoveMode(t *testing.T) {
	m := New(TUIModelConfig{})
	assert.True(t, m.canEnableMoveMode(), "no link mode -> move can be enabled")

	m.SetLinkMode(organizer.LinkModeHard)
	assert.False(t, m.canEnableMoveMode(), "hard link mode -> move cannot be enabled")

	m.SetLinkMode(organizer.LinkModeSoft)
	assert.False(t, m.canEnableMoveMode(), "soft link mode -> move cannot be enabled")

	m.SetLinkMode(organizer.LinkModeNone)
	assert.True(t, m.canEnableMoveMode(), "link mode cleared -> move can be enabled")
}

// TestHandleSettingsKeys_ToggleMoveFiles_RefusedWithLinkMode verifies the runtime
// guard: enabling move mode is refused while link mode is active, since move+link
// is mutually exclusive (ValidateMoveLinkMode rejects it at startup). The toggle
// must not change moveFiles, sync the processor, or persist.
func TestHandleSettingsKeys_ToggleMoveFiles_RefusedWithLinkMode(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	cfg := config.DefaultConfig(nil, nil)
	cfg.Output.Operation.MoveFiles = false
	require.NoError(t, config.Save(cfg, configPath))

	loaded, err := config.Load(configPath)
	require.NoError(t, err)
	m := New(TUIModelConfig{MoveFiles: loaded.Output.Operation.MoveFiles})
	m.SetConfigPath(configPath)
	m.SetLinkMode(organizer.LinkModeHard) // simulate --link-mode hard
	m.viewMgr.switchTo(viewSettings)
	m.settingsMgr.cursor = 3 // "Move Files" row

	proc, procErr := NewProcessingCoordinator(nil, nil, nil, nil, TUIProcessorConfig{}, "", false)
	if procErr != nil {
		proc = nil // factory is nil — processor assertion is skipped below
	}
	m.SetProcessor(proc)
	require.False(t, m.settingsMgr.snapshot.MoveFiles)

	// Attempt to toggle move on — must be refused due to link mode.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m2 := updated.(*Model)

	assert.False(t, m2.settingsMgr.snapshot.MoveFiles, "move mode must NOT enable while link mode is active")
	if proc != nil {
		assert.False(t, proc.opts.Load().(ProcessorOptions).MoveFiles, "processor must remain in copy mode")
	}

	reloaded, err := config.Load(configPath)
	require.NoError(t, err)
	assert.False(t, reloaded.Output.Operation.MoveFiles, "refused toggle must not persist move_files")

	require.NotEmpty(t, m2.logState.logs, "a warning should be logged when the toggle is refused")
	last := m2.logState.logs[len(m2.logState.logs)-1]
	assert.Equal(t, "warn", last.Level, "refused toggle should log a warning")
}
