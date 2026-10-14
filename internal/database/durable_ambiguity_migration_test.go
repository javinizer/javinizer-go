package database

import (
	"context"
	"database/sql"
	"testing"

	dbmigrations "github.com/javinizer/javinizer-go/internal/database/migrations"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

func TestDurableAmbiguityMigrationUpgradesAndRollsBack(t *testing.T) {
	db := newDatabaseTestDB(t)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, sqlDB, dbmigrations.Filesystem(), goose.WithTableName(schemaMigrationsTable), goose.WithDisableGlobalRegistry(true))
	require.NoError(t, err)
	ctx := context.Background()
	_, err = provider.DownTo(ctx, 17)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx, "INSERT INTO actresses (id,dmm_id,first_name,verified,origin) VALUES (1,77,'Ambiguous',0,'scrape'),(2,78,'Ordinary',0,'scrape')")
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx, "INSERT INTO movies (content_id,id,title) VALUES ('MIG-1','MIG-1','Migration')")
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx, "INSERT INTO movie_credits (id,movie_content_id,actress_id) VALUES (1,'MIG-1',1)")
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx, "INSERT INTO credit_collisions (credit_id,movie_content_id,field,status) VALUES (1,'MIG-1','identity_link','open')")
	require.NoError(t, err)
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	var ambiguous, ordinary bool
	require.NoError(t, sqlDB.QueryRowContext(ctx, "SELECT ambiguity_quarantined FROM actresses WHERE id=1").Scan(&ambiguous))
	require.NoError(t, sqlDB.QueryRowContext(ctx, "SELECT ambiguity_quarantined FROM actresses WHERE id=2").Scan(&ordinary))
	require.True(t, ambiguous)
	require.False(t, ordinary)
	_, err = provider.DownTo(ctx, 17)
	require.NoError(t, err)
	var count int
	err = sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('actresses') WHERE name='ambiguity_quarantined'").Scan(&count)
	require.NoError(t, err)
	require.Zero(t, count)
	_, err = sqlDB.ExecContext(ctx, "SELECT ambiguity_quarantined FROM actresses")
	require.Error(t, err)
	require.NotErrorIs(t, err, sql.ErrNoRows)
}
