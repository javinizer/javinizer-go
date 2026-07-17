package commandutil

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewQueryOnlyDependencies_Success(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "javinizer.db")
	cfg := &config.Config{
		Database: config.DatabaseConfig{Type: "sqlite", DSN: dbPath},
	}
	cfg.Metadata.R18DevDump.Path = filepath.Join(tmpDir, "nonexistent_dump.db")

	deps, err := NewQueryOnlyDependencies(cfg)
	require.NoError(t, err)
	require.NotNil(t, deps)
	assert.NotNil(t, deps.ScraperRegistry)
	assert.NotNil(t, deps.GetConfig())
	assert.NoError(t, deps.Close())

	_, statErr := os.Stat(dbPath)
	assert.True(t, os.IsNotExist(statErr))
}

func TestNewQueryOnlyDependencies_NilConfig(t *testing.T) {
	deps, err := NewQueryOnlyDependencies(nil)
	require.Error(t, err)
	assert.Nil(t, deps)
	assert.Contains(t, err.Error(), "config cannot be nil")
}

func TestNewQueryOnlyDependencies_RealScrapers(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Database: config.DatabaseConfig{Type: "sqlite", DSN: filepath.Join(tmpDir, "javinizer.db")},
	}
	cfg.Metadata.R18DevDump.Path = filepath.Join(tmpDir, "nonexistent_dump.db")

	deps, err := NewQueryOnlyDependencies(cfg)
	require.NoError(t, err)
	require.NotNil(t, deps)
	assert.NotNil(t, deps.ScraperRegistry)
	assert.NoError(t, deps.Close())

	_, statErr := os.Stat(filepath.Join(tmpDir, "javinizer.db"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestNewQueryOnlyDependencies_E2EInstanceWorks(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Database: config.DatabaseConfig{Type: "sqlite", DSN: filepath.Join(tmpDir, "javinizer.db")},
	}
	cfg.Metadata.R18DevDump.Path = filepath.Join(tmpDir, "nonexistent_dump.db")

	deps, err := NewQueryOnlyDependencies(cfg)
	require.NoError(t, err)
	require.NotNil(t, deps)

	instance, ok := deps.ScraperRegistry.GetInstance("e2emock")
	assert.True(t, ok)
	assert.NotNil(t, instance)
	assert.Equal(t, "e2emock", instance.Name())

	_ = context.Background()
	assert.NoError(t, deps.Close())
}
