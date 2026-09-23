package database

import (
	"context"
	"testing"

	dbmigrations "github.com/javinizer/javinizer-go/internal/database/migrations"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

func TestActressTranslationLanguageMigrationNormalizesDeduplicatesAndCycles(t *testing.T) {
	db := newDatabaseTestDB(t)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, sqlDB, dbmigrations.Filesystem(), goose.WithTableName(schemaMigrationsTable), goose.WithDisableGlobalRegistry(true))
	require.NoError(t, err)
	ctx := context.Background()
	_, err = provider.DownTo(ctx, 19)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx, "INSERT INTO actresses (id,first_name) VALUES (910001,'Migration')")
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx, "DROP INDEX IF EXISTS idx_actress_translations_actress_language")
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx, `INSERT INTO actress_translations (id,actress_id,language,display_name,source_name) VALUES
		(910001,910001,'en','keeper','provider-first'),
		(910002,910001,' EN ','duplicate','provider-second'),
		(910003,910001,' ZH-Hant-TW ','other','provider-third'),
		(910004,910001,'','empty','provider-empty'),
		(910005,910001,'   ','whitespace','provider-whitespace')`)
	require.NoError(t, err)
	_, err = provider.Up(ctx)
	require.NoError(t, err)

	rows, err := sqlDB.QueryContext(ctx, "SELECT id,language,display_name,source_name FROM actress_translations WHERE actress_id=910001 ORDER BY id")
	require.NoError(t, err)
	defer rows.Close()
	type row struct {
		id                        uint
		language, display, source string
	}
	var got []row
	for rows.Next() {
		var current row
		require.NoError(t, rows.Scan(&current.id, &current.language, &current.display, &current.source))
		got = append(got, current)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []row{{910001, "en", "keeper", "provider-first"}, {910003, "zh-hant-tw", "other", "provider-third"}}, got)
	_, err = sqlDB.ExecContext(ctx, "INSERT INTO actress_translations (actress_id,language) VALUES (910001,'en')")
	require.Error(t, err)

	_, err = provider.DownTo(ctx, 19)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx, "INSERT INTO actress_translations (actress_id,language) VALUES (910001,''),(910001,'   ')")
	require.NoError(t, err)
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	var count int
	require.NoError(t, sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM actress_translations WHERE actress_id=910001").Scan(&count))
	require.Equal(t, 2, count)
}
