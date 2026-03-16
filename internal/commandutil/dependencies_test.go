package commandutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewDependencies_Success tests successful dependency initialization
func TestNewDependencies_Success(t *testing.T) {
	tmpDir := t.TempDir()
	_, cfg := createTestConfig(t,
		WithDatabaseDSN(filepath.Join(tmpDir, "test.db")),
	)

	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	require.NotNil(t, deps)
	defer func() { _ = deps.Close() }()

	assert.NotNil(t, deps.Config)
	assert.NotNil(t, deps.DB)
	assert.NotNil(t, deps.ScraperRegistry)
	assert.Equal(t, cfg, deps.Config)
}

// TestNewDependencies_NilConfig tests error when config is nil
func TestNewDependencies_NilConfig(t *testing.T) {
	deps, err := NewDependencies(nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be nil")
	assert.Nil(t, deps)
}

// TestNewDependencies_DBDirCreationFails tests error when database directory creation fails
func TestNewDependencies_DBDirCreationFails(t *testing.T) {
	// Use a path that will fail directory creation (e.g., under a read-only file)
	tmpDir := t.TempDir()
	readOnlyFile := filepath.Join(tmpDir, "readonly.txt")
	err := os.WriteFile(readOnlyFile, []byte("test"), 0444)
	require.NoError(t, err)

	// Try to create DB in a subdirectory of a file (will fail)
	invalidDBPath := filepath.Join(readOnlyFile, "subdir", "test.db")
	cfg := config.DefaultConfig()
	cfg.Database.DSN = invalidDBPath

	deps, err := NewDependencies(cfg)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "create database directory")
	assert.Nil(t, deps)
}

// TestNewDependencies_DBInitFails tests error when database initialization fails
func TestNewDependencies_DBInitFails(t *testing.T) {
	// Create a read-only directory that will prevent DB creation
	tmpDir := t.TempDir()
	dbDir := filepath.Join(tmpDir, "readonly_dir")
	err := os.MkdirAll(dbDir, 0555) // Read-only directory
	require.NoError(t, err)

	cfg := config.DefaultConfig()
	cfg.Database.DSN = filepath.Join(dbDir, "test.db")

	deps, err := NewDependencies(cfg)

	// Should fail either during dir creation or DB init
	assert.Error(t, err)
	assert.Nil(t, deps)
}

// TestNewDependencies_MigrationSuccess tests that migrations run successfully
func TestNewDependencies_MigrationSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	_, cfg := createTestConfig(t,
		WithDatabaseDSN(filepath.Join(tmpDir, "test.db")),
	)

	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	require.NotNil(t, deps)
	defer func() { _ = deps.Close() }()

	// Verify that tables were created by checking if we can query them
	err = deps.DB.DB.Exec("SELECT COUNT(*) FROM movies").Error
	assert.NoError(t, err) // Table should exist after migration
}

// TestClose_Success tests successful cleanup
func TestClose_Success(t *testing.T) {
	tmpDir := t.TempDir()
	_, cfg := createTestConfig(t,
		WithDatabaseDSN(filepath.Join(tmpDir, "test.db")),
	)

	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	require.NotNil(t, deps)

	// Close should succeed
	err = deps.Close()
	assert.NoError(t, err)
}

// TestClose_NilDB tests graceful handling when DB is nil
func TestClose_NilDB(t *testing.T) {
	deps := &Dependencies{
		DB: nil,
	}

	// Should not panic or error
	err := deps.Close()
	assert.NoError(t, err)
}

// TestClose_MultipleCalls tests that Close can be called multiple times
func TestClose_MultipleCalls(t *testing.T) {
	tmpDir := t.TempDir()
	_, cfg := createTestConfig(t,
		WithDatabaseDSN(filepath.Join(tmpDir, "test.db")),
	)

	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	require.NotNil(t, deps)

	// First close should succeed
	err = deps.Close()
	assert.NoError(t, err)

	// Second close should also succeed (idempotent behavior)
	err = deps.Close()
	assert.NoError(t, err)
}
