package word_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestWordListReadOnlyEmptyCatalogAndWritableRecovery(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("JAVINIZER_DB", "")
	t.Setenv("JAVINIZER_R18DEV_DUMP_PATH", "")
	dbPath := filepath.Join(dir, "catalog.sqlite")
	db, err := database.New(&database.Config{Type: "sqlite", DSN: dbPath, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	// A freshly migrated database is valid before best-effort default seeding.
	// Reopen this complete schema using SQLite's native read-only URI mode.
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	rows, err := database.NewWordReplacementRepository(db).List(t.Context())
	require.NoError(t, err)
	require.Empty(t, rows)
	require.NoError(t, db.Close())
	before, err := os.ReadFile(dbPath)
	require.NoError(t, err)

	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = "file:catalog.sqlite?mode=ro"
	cfg.System.TempDir = filepath.Join(dir, "temp")
	cfg.System.VersionCheckEnabled = false
	configPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, config.Save(cfg, configPath))

	readOnly, err := database.New(&database.Config{Type: "sqlite", DSN: cfg.Database.DSN, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = readOnly.Close() })
	probe := &models.WordReplacement{Original: "readonly-probe", Replacement: "must not be inserted"}
	err = database.NewWordReplacementRepository(readOnly).Create(t.Context(), probe)
	var sqliteErr sqlite3.Error
	require.ErrorAs(t, err, &sqliteErr)
	require.Equal(t, sqlite3.ErrReadonly, sqliteErr.Code)
	require.NoError(t, readOnly.Close())

	console, output, err := executePersistedCatalogCLI(t, configPath, "list")
	require.NoError(t, err)
	require.Empty(t, output)
	require.Contains(t, console, "No word replacements configured\n")
	require.NotContains(t, console, "=== Word Replacements ===")
	require.NotContains(t, console, "Total:")
	after, err := os.ReadFile(dbPath)
	require.NoError(t, err)
	require.Equal(t, before, after, "listing must preserve the on-disk database when default seeding is refused")

	check, err := database.New(&database.Config{Type: "sqlite", DSN: cfg.Database.DSN, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = check.Close() })
	rows, err = database.NewWordReplacementRepository(check).List(t.Context())
	require.NoError(t, err)
	require.Empty(t, rows, "read-only startup must not seed any replacements")
	require.NoError(t, check.Close())

	// Restoring write access must permit normal startup seeding and listing.
	cfg.Database.DSN = "file:catalog.sqlite?mode=rw"
	require.NoError(t, config.Save(cfg, configPath))
	console, output, err = executePersistedCatalogCLI(t, configPath, "list")
	require.NoError(t, err)
	require.Empty(t, output)
	require.Contains(t, console, "=== Word Replacements ===")
	require.NotContains(t, console, "No word replacements configured")
	recovered, err := database.New(&database.Config{Type: "sqlite", DSN: dbPath, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = recovered.Close() })
	rows, err = database.NewWordReplacementRepository(recovered).List(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	require.Contains(t, console, fmt.Sprintf("Total: %d replacements\n", len(rows)))
	for _, row := range rows {
		require.True(t, database.IsDefaultWordReplacement(row.Original))
		require.Contains(t, console, row.Original)
		require.Contains(t, console, row.Replacement)
	}
}
