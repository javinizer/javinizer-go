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

func TestMigrations_EnvelopeGenerationColumnUpDown(t *testing.T) {
	db := newDatabaseTestDB(t)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	provider, err := goose.NewProvider(
		goose.DialectSQLite3,
		sqlDB,
		dbmigrations.Filesystem(),
		goose.WithTableName(schemaMigrationsTable),
		goose.WithGoMigrations(dbmigrations.GoMigrations()...),
		goose.WithDisableGlobalRegistry(true),
	)
	require.NoError(t, err)

	ctx := context.Background()
	latest, err := provider.GetDBVersion(ctx)
	require.NoError(t, err)
	require.Greater(t, latest, int64(0))
	assert.Contains(t, jobColumnNames(t, sqlDB), "envelope_generation")

	// Down to one version below the envelope migration (000014). Migrations
	// may be appended after envelope_generation (e.g. 000015), so target the
	// version explicitly instead of DownTo(latest-1).
	_, err = provider.DownTo(ctx, 13)
	require.NoError(t, err)
	assert.NotContains(t, jobColumnNames(t, sqlDB), "envelope_generation")

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assert.Contains(t, jobColumnNames(t, sqlDB), "envelope_generation")

	var generation sql.NullInt64
	err = sqlDB.QueryRowContext(ctx, "SELECT envelope_generation FROM jobs WHERE id = ?", "missing-p6-job").Scan(&generation)
	require.Error(t, err)
	require.False(t, generation.Valid)
}
