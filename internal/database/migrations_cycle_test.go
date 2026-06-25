package database

import (
	"context"
	"database/sql"
	"testing"

	dbmigrations "github.com/javinizer/javinizer-go/internal/database/migrations"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jobColumnNames returns the column names of the jobs table in creation order.
func jobColumnNames(t *testing.T, sqlDB *sql.DB) []string {
	t.Helper()
	rows, err := sqlDB.QueryContext(context.Background(), "PRAGMA table_info(jobs)")
	require.NoError(t, err)
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		require.NoError(t, rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk))
		cols = append(cols, name)
	}
	require.NoError(t, rows.Err())
	return cols
}

// newMigrationProvider builds a goose provider over the embedded SQL migrations,
// mirroring the configuration used by migrations_runner.go (RunMigrationsOnStartup)
// but without the startup-only baseline-hash bookkeeping so the full Up/Down cycle
// can be exercised.
func newMigrationProvider(t *testing.T, sqlDB *sql.DB) *goose.Provider {
	t.Helper()
	provider, err := goose.NewProvider(
		goose.DialectSQLite3,
		sqlDB,
		dbmigrations.Filesystem(),
		goose.WithTableName(schemaMigrationsTable),
		goose.WithGoMigrations(dbmigrations.GoMigrations()...),
		goose.WithDisableGlobalRegistry(true),
	)
	require.NoError(t, err)
	return provider
}

// TestMigrations_Up_Down_Up_Cycle locks in a regression fix for the 000008 Down
// migration. Before the fix, 000008 Down referenced operation_mode_override — a
// column owned by the later 000010 migration. On rollback goose runs migrations in
// reverse order, so 000010 Down runs first and rebuilds jobs without that column,
// then 000008 Down fails with "no such column: operation_mode_override". This test
// runs the full Up -> Down-to-zero -> Up cycle on a :memory: SQLite database and
// asserts the schema is valid at every step. It would fail on the pre-fix code.
func TestMigrations_Up_Down_Up_Cycle(t *testing.T) {
	// :memory: is normalised to a shared-cache memory URI by New() so the in-memory
	// database survives goose opening multiple connections from the same *sql.DB.
	db := newDatabaseTestDB(t)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	ctx := context.Background()
	provider := newMigrationProvider(t, sqlDB)

	// newDatabaseTestDB already ran RunMigrationsOnStartup (Up). Confirm the
	// post-Up jobs schema includes both 000008's and 000010's columns.
	upCols := jobColumnNames(t, sqlDB)
	assert.Contains(t, upCols, "update", "000008 Up should add the update column")
	assert.Contains(t, upCols, "operation_mode_override", "000010 Up should add operation_mode_override")

	// Roll every migration back down to (but not including) version 0. This is the
	// path that broke before the fix: 000010 Down runs, then 000008 Down runs.
	if _, err := provider.DownTo(ctx, 0); err != nil {
		t.Fatalf("rolling all migrations down to 0: %v (this fails on the pre-fix 000008 Down)", err)
	}

	// After a full rollback the jobs table should no longer exist.
	var name string
	err = sqlDB.QueryRowContext(ctx,
		"SELECT name FROM sqlite_master WHERE type='table' AND name='jobs'").Scan(&name)
	require.ErrorIs(t, err, sql.ErrNoRows, "jobs table should be dropped after full rollback")

	// Re-apply all migrations from scratch. This proves the Down path left the
	// database in a state from which Up can cleanly rebuild the full schema.
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("re-running Up after full rollback: %v", err)
	}

	// Final schema must match the original post-Up schema.
	finalCols := jobColumnNames(t, sqlDB)
	assert.Equal(t, upCols, finalCols, "jobs schema after Up->Down->Up must match the initial Up schema")
	assert.Contains(t, finalCols, "update")
	assert.Contains(t, finalCols, "operation_mode_override")

	// Sanity: the migration version table should report the latest version.
	latest, err := provider.GetDBVersion(ctx)
	require.NoError(t, err)
	assert.Greater(t, latest, int64(0), "DB version should be > 0 after re-applying migrations")
}
