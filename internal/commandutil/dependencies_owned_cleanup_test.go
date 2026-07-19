package commandutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ownedCleanupInvalidCfg(dsn string) *config.Config {
	cfg := &config.Config{Database: config.DatabaseConfig{Type: "sqlite", DSN: dsn}}
	cfg.Scrapers.Priority = []string{"r18dev"}
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Enabled: true, RateLimit: -1},
	}
	return cfg
}

func ownedCleanupR18devRegistry(t *testing.T) *scraperutil.ScraperRegistry {
	t.Helper()
	reg := scraperutil.NewScraperRegistry()
	reg.Register(scraperutil.ScraperRegistration{
		Name:     "r18dev",
		Defaults: models.ScraperSettings{Enabled: true, Language: "en"},
	})
	return reg
}

func TestNewDependenciesWithOptions_OwnedDBInjectedRegistryValidationFails_ClosesDB(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "owned_injected_reg.db")
	cfg := ownedCleanupInvalidCfg(dsn)
	reg := ownedCleanupR18devRegistry(t)

	deps, err := NewDependenciesWithOptions(cfg, &DependenciesOptions{ScraperRegistry: reg})
	require.Error(t, err)
	assert.Nil(t, deps)
	assert.Contains(t, err.Error(), "rate_limit")

	assert.FileExists(t, dsn, "owned DB file should be created before injected registry validation")
	_, walErr := os.Stat(dsn + "-wal")
	assert.True(t, os.IsNotExist(walErr), "owned DB must be closed on failure (WAL sidecar removed)")
}

func TestNewDependenciesWithOptions_OwnedDBRealRegistryFinalizeFails_ClosesDB(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "owned_real_reg.db")
	cfg := ownedCleanupInvalidCfg(dsn)

	deps, err := NewDependenciesWithOptions(cfg, nil)
	require.Error(t, err)
	assert.Nil(t, deps)
	assert.Contains(t, err.Error(), "rate_limit")

	assert.FileExists(t, dsn, "owned DB file should be created before real registry finalize")
	_, walErr := os.Stat(dsn + "-wal")
	assert.True(t, os.IsNotExist(walErr), "owned DB must be closed on failure (WAL sidecar removed)")
}

func TestNewDependenciesWithOptions_InjectedDBInjectedRegistryValidationFails_PreservesDB(t *testing.T) {
	cfg := ownedCleanupInvalidCfg(":memory:")
	reg := ownedCleanupR18devRegistry(t)
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	deps, err := NewDependenciesWithOptions(cfg, &DependenciesOptions{DB: db, ScraperRegistry: reg})
	require.Error(t, err)
	assert.Nil(t, deps)
	assert.Contains(t, err.Error(), "rate_limit")

	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	assert.NoError(t, sqlDB.Ping(), "injected DB must not be closed by the constructor")
}
