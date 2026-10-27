package scrape_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/scrape"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper/e2emock"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func textLifecycleConfig(t *testing.T) (string, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")
	t.Setenv("JAVINIZER_DB", "")
	t.Setenv("JAVINIZER_DATA_DIR", dir)
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = filepath.Join(dir, "movies.db")
	cfg.Scrapers.Priority = []string{e2emock.Name}
	cfg.Metadata.Priority.Priority = []string{e2emock.Name}
	cfg.Metadata.R18DevDump.Enabled = false
	cfg.Metadata.Translation.Enabled = false
	cfg.System.VersionCheckEnabled = false
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, config.Save(cfg, path))
	return path, cfg
}

// Exercise the public Cobra command with real owned dependencies and the
// existing offline scraper fixture, rather than replacing bootstrap functions.
func TestTextScrapeOwnedDependenciesPersistAndClose(t *testing.T) {
	configPath, cfg := textLifecycleConfig(t)
	var out, stderr bytes.Buffer
	root := &cobra.Command{Use: "javinizer", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("config", configPath, "config file")
	root.SetOut(&out)
	root.SetErr(&stderr)
	root.AddCommand(scrape.NewCommand())
	root.SetArgs([]string{"scrape", "GOOD-701", "--scrapers", e2emock.Name})

	require.NoError(t, root.ExecuteContext(t.Context()))
	assert.Empty(t, stderr.String())
	for _, value := range []string{"GOOD-701", "E2E Movie GOOD-701", "E2E Test Studio", e2emock.Name} {
		assert.Contains(t, out.String(), value, "text output should include scraped metadata")
	}
	require.FileExists(t, cfg.Database.DSN)
	// File-backed SQLite uses WAL while open; a completed owned command must
	// release its handle before returning, leaving no WAL/SHM sidecars.
	for _, suffix := range []string{"-wal", "-shm"} {
		_, err := os.Stat(cfg.Database.DSN + suffix)
		require.True(t, os.IsNotExist(err), "owned database sidecar remains: %s (%v)", suffix, err)
	}

	db, err := database.New(&database.Config{Type: "sqlite", DSN: cfg.Database.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	stored, err := database.NewMovieRepository(db).FindByID(t.Context(), "GOOD-701")
	require.NoError(t, err)
	assert.Equal(t, "GOOD-701", stored.ContentID)
	assert.Equal(t, "E2E Movie GOOD-701", stored.Title)
	assert.Equal(t, e2emock.Name, stored.SourceName)
	var count int64
	require.NoError(t, db.Model(&models.Movie{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestTextScrapeEmptyIDReturnsValidationErrorAndCloses(t *testing.T) {
	configPath, cfg := textLifecycleConfig(t)
	var out bytes.Buffer
	cmd := scrape.NewCommand()
	cmd.Flags().String("config", configPath, "config file")
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	// Cobra accepts one empty positional argument; the real workflow must
	// reject it as an empty MovieID instead of returning a formatted success.
	cmd.SetArgs([]string{""})
	err := cmd.ExecuteContext(t.Context())
	require.EqualError(t, err, "scrape called with empty MovieID")
	assert.Empty(t, out.String())
	require.FileExists(t, cfg.Database.DSN, "validation runs after real dependency bootstrap")
	_, statErr := os.Stat(cfg.Database.DSN + "-wal")
	assert.True(t, os.IsNotExist(statErr), "owned database must close on validation failure")
}

func TestRunCanceledContextPreservesBorrowedDependencies(t *testing.T) {
	configPath, cfg := textLifecycleConfig(t)
	deps, err := commandutil.NewDependencies(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, deps.Close()) })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cmd := scrape.NewCommand()
	require.NoError(t, cmd.Flags().Set("scrapers", e2emock.Name))

	movie, results, err := scrape.Run(ctx, cmd, []string{"GOOD-702"}, configPath, deps)
	require.EqualError(t, err, "scrape timed out")
	assert.Nil(t, movie)
	assert.Nil(t, results)
	assert.ErrorIs(t, ctx.Err(), context.Canceled)
	sqlDB, err := deps.DB.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.PingContext(t.Context()), "Run must not close borrowed dependencies")
	var count int64
	require.NoError(t, deps.DB.Model(&models.Movie{}).Count(&count).Error)
	assert.Zero(t, count, "a canceled scrape must not persist a movie")

	// The same dependencies remain usable by the owner after cancellation.
	movie, results, err = scrape.Run(t.Context(), cmd, []string{"GOOD-702"}, configPath, deps)
	require.NoError(t, err)
	require.NotNil(t, movie)
	assert.Equal(t, "E2E Movie GOOD-702", movie.Title)
	require.Len(t, results, 1)
	assert.Equal(t, e2emock.Name, results[0].Source)
	require.NoError(t, deps.DB.Model(&models.Movie{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}
