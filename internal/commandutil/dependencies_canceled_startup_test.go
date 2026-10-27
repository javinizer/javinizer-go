package commandutil_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDependenciesCanceledStartupClosesOwnedDatabase(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = filepath.Join(t.TempDir(), "movies.db")
	cfg.Metadata.R18DevDump.Enabled = false

	// A brand-new, empty SQLite file does not create WAL sidecars merely by
	// opening it. Seed a real schema so the constructor's initial schema read
	// creates observable sidecars even if migration locking is canceled first.
	seedDB, err := database.New(&database.Config{Type: "sqlite", DSN: cfg.Database.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, seedDB.Close()) })
	require.NoError(t, seedDB.Exec("CREATE TABLE startup_probe (value TEXT NOT NULL)").Error)
	require.NoError(t, seedDB.Exec("INSERT INTO startup_probe (value) VALUES ('preserved')").Error)
	require.NoError(t, seedDB.Close())

	// Establish that this exact public constructor leaves sidecars while a
	// handle is live; their later absence is not a vacuous cleanup assertion.
	controlDB, err := database.New(&database.Config{Type: "sqlite", DSN: cfg.Database.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, controlDB.Close()) })
	for _, suffix := range []string{"-wal", "-shm"} {
		require.FileExists(t, cfg.Database.DSN+suffix)
	}
	require.NoError(t, controlDB.Close())
	for _, suffix := range []string{"-wal", "-shm"} {
		require.NoFileExists(t, cfg.Database.DSN+suffix)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// Only the supported startup context is injected. Opening SQLite,
	// migrations, and ownership cleanup use the real constructor path.
	deps, err := commandutil.NewDependenciesWithOptions(cfg, &commandutil.DependenciesOptions{Ctx: ctx})
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "failed to run migrations:")
	assert.Nil(t, deps, "partially initialized dependencies must not escape")
	require.FileExists(t, cfg.Database.DSN, "cancellation must preserve the existing database")
	for _, suffix := range []string{"-wal", "-shm"} {
		_, statErr := os.Stat(cfg.Database.DSN + suffix)
		require.True(t, os.IsNotExist(statErr), "owned database sidecar remains: %s (%v)", suffix, statErr)
	}

	// Cancellation must leave the same database recoverable: a fresh startup
	// context can migrate it and use the normal application schema.
	deps, err = commandutil.NewDependenciesWithOptions(cfg, &commandutil.DependenciesOptions{Ctx: t.Context()})
	require.NoError(t, err)
	require.NotNil(t, deps)
	t.Cleanup(func() { require.NoError(t, deps.Close()) })
	var count int64
	require.NoError(t, deps.DB.Model(&models.Movie{}).Count(&count).Error)
	assert.Zero(t, count)
	var preserved string
	require.NoError(t, deps.DB.Raw("SELECT value FROM startup_probe").Scan(&preserved).Error)
	assert.Equal(t, "preserved", preserved, "cancellation and retry must retain existing data")
}
