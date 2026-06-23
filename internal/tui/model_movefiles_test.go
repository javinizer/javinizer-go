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

// TestSetMoveFilesSyncsModel verifies SetMoveFiles updates the model state.
func TestSetMoveFilesSyncsModel(t *testing.T) {
	cfg := config.DefaultConfig()
	m := New(cfg)

	m.SetMoveFiles(true)
	assert.True(t, m.moveFiles)

	m.SetMoveFiles(false)
	assert.False(t, m.moveFiles)
}

// TestSaveConfigPersistsMoveFiles verifies that toggling moveFiles and calling
// saveConfig writes move_files to config.yaml so it survives a restart (issue #36).
func TestSaveConfigPersistsMoveFiles(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := config.DefaultConfig()
	cfg.Output.MoveFiles = false
	m := New(cfg)
	m.SetConfigPath(configPath)

	// Simulate the TUI toggle: flip moveFiles on and persist
	m.moveFiles = true
	m.saveConfig()

	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "move_files: true",
		"move_files should be persisted as true after saveConfig")
}

// TestSaveConfigNoOpWithoutPath verifies saveConfig is a no-op when no config path is set
// (e.g. config not yet written), so it never crashes the TUI.
func TestSaveConfigNoOpWithoutPath(t *testing.T) {
	cfg := config.DefaultConfig()
	m := New(cfg)
	// configPath is empty by default
	m.moveFiles = true
	assert.NotPanics(t, func() { m.saveConfig() })
}
