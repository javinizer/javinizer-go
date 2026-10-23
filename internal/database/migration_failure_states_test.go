package database

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/mattn/go-sqlite3"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigrationRunnerRejectsDedicatedConnectionHandle(t *testing.T) {
	db := newDatabaseTestDB(t)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	before, err := GetStoredHash(sqlDB, "000001_baseline.sql")
	require.NoError(t, err)
	err = db.Connection(func(connection *gorm.DB) error {
		// GORM's supported dedicated-connection API exposes *sql.Conn, not *sql.DB.
		// Startup migration needs the pool and must reject this valid connection view.
		view := &DB{DB: connection, dsn: db.dsn, fs: db.fs}
		return view.RunMigrationsOnStartup(context.Background())
	})
	require.ErrorIs(t, err, gorm.ErrInvalidDB)
	require.EqualError(t, err, "get sql database handle: "+gorm.ErrInvalidDB.Error())
	after, err := GetStoredHash(sqlDB, "000001_baseline.sql")
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestMigrationRunnerLockFailuresPreserveApplicationState(t *testing.T) {
	for _, stage := range []string{"directory policy", "canceled acquisition"} {
		t.Run(stage, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "application.db")
			db, err := New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			require.NoError(t, db.Exec("CREATE TABLE application_state (value TEXT NOT NULL)").Error)
			require.NoError(t, db.Exec("INSERT INTO application_state VALUES ('preserved')").Error)
			ctx := context.Background()
			if stage == "directory policy" {
				db.fs = afero.NewReadOnlyFs(afero.NewOsFs())
			} else {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			err = db.RunMigrationsOnStartup(ctx)
			if stage == "directory policy" {
				require.ErrorIs(t, err, os.ErrPermission)
				require.EqualError(t, err, "initialize startup migration lock: create migration lock directory: "+syscall.EPERM.Error())
			} else {
				require.ErrorIs(t, err, context.Canceled)
				require.EqualError(t, err, "acquire startup migration lock: context canceled")
			}
			var value string
			require.NoError(t, db.Raw("SELECT value FROM application_state").Scan(&value).Error)
			require.Equal(t, "preserved", value)
			require.False(t, db.Migrator().HasTable("movies"))
			require.NoFileExists(t, path+".migration.lock")
		})
	}
}

func TestMigrationRunnerReportsMalformedPersistedHashSchema(t *testing.T) {
	db := newDatabaseTestDB(t)
	require.NoError(t, db.Exec("ALTER TABLE schema_migrations_hash RENAME COLUMN content_hash TO archived_hash").Error)
	var before string
	require.NoError(t, db.Raw("SELECT archived_hash FROM schema_migrations_hash WHERE migration_name = ?", "000001_baseline.sql").Scan(&before).Error)
	err := db.RunMigrationsOnStartup(context.Background())
	require.EqualError(t, err, "get stored baseline hash: get stored hash: no such column: content_hash")
	var sqliteErr sqlite3.Error
	require.ErrorAs(t, err, &sqliteErr)
	require.Equal(t, sqlite3.ErrError, sqliteErr.Code)
	var after string
	require.NoError(t, db.Raw("SELECT archived_hash FROM schema_migrations_hash WHERE migration_name = ?", "000001_baseline.sql").Scan(&after).Error)
	require.Equal(t, before, after)
}

func TestMigrationRunnerReportsRejectedFinalHashWithoutInventingSuccess(t *testing.T) {
	db, err := New(&Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	require.NoError(t, ensureMigrationHashTable(sqlDB))
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_baseline_hash BEFORE INSERT ON schema_migrations_hash BEGIN SELECT RAISE(ABORT, 'fixture baseline hash rejected'); END`).Error)
	err = db.RunMigrationsOnStartup(context.Background())
	require.EqualError(t, err, "store baseline hash: store migration hash: fixture baseline hash rejected")
	var sqliteErr sqlite3.Error
	require.ErrorAs(t, err, &sqliteErr)
	require.Equal(t, sqlite3.ErrConstraintTrigger, sqliteErr.ExtendedCode)
	hash, err := GetStoredHash(sqlDB, "000001_baseline.sql")
	require.NoError(t, err)
	require.Empty(t, hash)
	pending, err := newMigrationProvider(t, sqlDB).HasPending(context.Background())
	require.NoError(t, err)
	require.False(t, pending, "the final hash write is after schema migration, not in its transaction")
	require.NoError(t, db.Exec("DROP TRIGGER reject_baseline_hash").Error)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()), "the next startup can complete hash bookkeeping")
	hash, err = GetStoredHash(sqlDB, "000001_baseline.sql")
	require.NoError(t, err)
	require.Len(t, hash, 64)
}

func TestMigrationRunnerRejectedVersionRollsBackInMemoryMigration(t *testing.T) {
	db, err := New(&Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	provider := newMigrationProvider(t, sqlDB)
	pending, err := provider.HasPending(context.Background())
	require.NoError(t, err)
	require.True(t, pending)
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_first_version BEFORE INSERT ON schema_migrations WHEN NEW.version_id = 1 BEGIN SELECT RAISE(ABORT, 'fixture migration version rejected'); END`).Error)
	err = db.RunMigrationsOnStartup(context.Background())
	require.Error(t, err)
	require.True(t, strings.HasPrefix(err.Error(), "database migration failed: "), err)
	require.NotContains(t, err.Error(), "backup created")
	var sqliteErr sqlite3.Error
	require.ErrorAs(t, err, &sqliteErr)
	require.Equal(t, sqlite3.ErrConstraintTrigger, sqliteErr.ExtendedCode)
	require.EqualError(t, sqliteErr, "fixture migration version rejected")
	require.False(t, db.Migrator().HasTable("movies"), "baseline table creation and version insertion must roll back together")
	var applied int
	require.NoError(t, sqlDB.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version_id > 0").Scan(&applied))
	require.Zero(t, applied)
	require.NoError(t, db.Exec("DROP TRIGGER reject_first_version").Error)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
}

func TestBackupDirectoryPolicyPreservesSourceRows(t *testing.T) {
	db := newDatabaseTestDB(t)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	before, err := GetStoredHash(sqlDB, "000001_baseline.sql")
	require.NoError(t, err)
	_, err = sqlDB.Exec("CREATE TABLE backup_application_state (value TEXT NOT NULL)")
	require.NoError(t, err)
	_, err = sqlDB.Exec("INSERT INTO backup_application_state VALUES (?)", "preserved application row")
	require.NoError(t, err)
	var beforeRow string
	require.NoError(t, sqlDB.QueryRow("SELECT value FROM backup_application_state").Scan(&beforeRow))
	path := filepath.Join(t.TempDir(), "snapshot.db")
	backup, err := createSQLiteBackupSnapshot(context.Background(), sqlDB, path, afero.NewReadOnlyFs(afero.NewOsFs()))
	require.Empty(t, backup)
	require.ErrorIs(t, err, os.ErrPermission)
	require.EqualError(t, err, "create backup directory: "+syscall.EPERM.Error())
	after, err := GetStoredHash(sqlDB, "000001_baseline.sql")
	require.NoError(t, err)
	require.Equal(t, before, after)
	var afterRow string
	require.NoError(t, sqlDB.QueryRow("SELECT value FROM backup_application_state").Scan(&afterRow))
	require.Equal(t, beforeRow, afterRow)
	matches, err := filepath.Glob(path + ".*.backup")
	require.NoError(t, err)
	require.Empty(t, matches)
	require.NoError(t, sqlDB.Ping(), "a failed backup must not poison the source connection")
}
