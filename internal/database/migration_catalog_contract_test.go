package database

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gofrs/flock"
	"github.com/stretchr/testify/require"
)

func TestStartupMigrationRejectsDuplicateCatalogBeforeBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "application.db")
	db, err := New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TABLE application_state (value TEXT NOT NULL)").Error)
	require.NoError(t, db.Exec("INSERT INTO application_state VALUES ('preserved')").Error)

	catalog := t.TempDir()
	const first = "000001_first.sql"
	const second = "000001_second.sql"
	sql := []byte("-- +goose Up\nUPDATE application_state SET value = 'changed';\n")
	for _, name := range []string{first, second} {
		require.NoError(t, os.WriteFile(filepath.Join(catalog, name), sql, 0600))
	}
	locker, err := newStartupMigrationLocker(path, db.fs)
	require.NoError(t, err)
	observer := &migrationReleaseObserver{Locker: locker}

	err = db.runMigrationsWithLocker(t.Context(), sqlDB, observer, os.DirFS(catalog))

	cause := errors.Unwrap(err)
	require.ErrorContains(t, cause, "found duplicate migration version 1:")
	require.ErrorContains(t, cause, first)
	require.ErrorContains(t, cause, second)
	require.EqualError(t, err, "initialize migration provider: "+cause.Error())

	// The hash table is ensured before provider initialization. Nothing past
	// provider initialization should read pending work, back up, or apply SQL.
	require.True(t, db.Migrator().HasTable("schema_migrations_hash"))
	hash, err := GetStoredHash(sqlDB, "000001_baseline.sql")
	require.NoError(t, err)
	require.Empty(t, hash)
	require.False(t, db.Migrator().HasTable(schemaMigrationsTable))
	backups, err := filepath.Glob(path + ".*.backup")
	require.NoError(t, err)
	require.Empty(t, backups)
	var value string
	require.NoError(t, sqlDB.QueryRow("SELECT value FROM application_state").Scan(&value))
	require.Equal(t, "preserved", value)

	require.Equal(t, 1, observer.unlockCalls)
	probe := flock.New(path + ".migration.lock")
	t.Cleanup(func() { require.NoError(t, probe.Close()) })
	acquired, err := probe.TryLock()
	require.NoError(t, err)
	require.True(t, acquired, "the provider initialization failure must release the real startup lock")
}
