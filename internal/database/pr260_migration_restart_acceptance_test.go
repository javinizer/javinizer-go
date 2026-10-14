package database

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestPR260MigrationRestartAcceptance_FileBackedRestoreAndReopen(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	legacyPath := filepath.Join(tmpDir, "legacy.db")
	config := func(path string) *Config {
		return &Config{Type: "sqlite", DSN: path, LogLevel: "error"}
	}
	movieID := "pr260-migration-restart"
	const sourceID uint = 101
	const missingAID uint = 102
	const missingBID uint = 103
	const targetID uint = 104
	const sourceDMMID = 81001

	countFor := func(conn *sql.DB, query string, args ...any) int64 {
		var count int64
		require.NoError(t, conn.QueryRowContext(ctx, query, args...).Scan(&count))
		return count
	}
	idsFor := func(conn *sql.DB, query string) []uint {
		rows, err := conn.QueryContext(ctx, query, movieID)
		require.NoError(t, err)
		var ids []uint
		for rows.Next() {
			var id uint
			require.NoError(t, rows.Scan(&id))
			ids = append(ids, id)
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		return ids
	}
	assertForeignKeys := func(conn *sql.DB) {
		rows, err := conn.QueryContext(ctx, "PRAGMA foreign_key_check")
		require.NoError(t, err)
		require.False(t, rows.Next())
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
	}
	assertProjection := func(conn *sql.DB, expected []uint) {
		projected := idsFor(conn, "SELECT actress_id FROM movie_actresses WHERE movie_content_id = ? ORDER BY actress_id")
		credits := idsFor(conn, "SELECT mc.actress_id FROM movie_credits mc JOIN actresses a ON a.id = mc.actress_id WHERE mc.movie_content_id = ? AND mc.suppressed = 0 AND a.verified = 1 ORDER BY mc.actress_id")
		require.ElementsMatch(t, expected, projected)
		require.ElementsMatch(t, expected, credits)
	}
	readGeneration := func(conn *sql.DB) (bool, int64) {
		var dirty bool
		var generation int64
		require.NoError(t, conn.QueryRowContext(ctx, "SELECT render_dirty, render_generation FROM movies WHERE content_id = ?", movieID).Scan(&dirty, &generation))
		return dirty, generation
	}
	assertMigrated := func(conn *sql.DB) {
		require.Equal(t, int64(3), countFor(conn, "SELECT COUNT(*) FROM movie_credits WHERE movie_content_id = ?", movieID))
		require.Equal(t, int64(3), countFor(conn, "SELECT COUNT(*) FROM (SELECT movie_content_id, actress_id FROM movie_credits WHERE movie_content_id = ? GROUP BY movie_content_id, actress_id)", movieID))
		require.Equal(t, int64(3), countFor(conn, "SELECT COUNT(*) FROM movie_credits WHERE movie_content_id = ? AND legacy_inferred = 1 AND origin = 'user'", movieID))
		require.Equal(t, int64(2), countFor(conn, "SELECT COUNT(*) FROM actresses WHERE dmm_id = 0"))
		assertProjection(conn, []uint{sourceID, missingAID, missingBID})
		assertForeignKeys(conn)
	}

	legacy, err := New(config(legacyPath))
	require.NoError(t, err)
	defer func() { _ = legacy.Close() }()
	legacySQL, err := legacy.DB.DB()
	require.NoError(t, err)
	provider := newMigrationProvider(t, legacySQL)
	_, err = provider.UpTo(ctx, 15)
	require.NoError(t, err)
	require.Equal(t, int64(0), countFor(legacySQL, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'movie_credits'"))
	_, err = legacySQL.ExecContext(ctx, `
		INSERT INTO movies (content_id, id, title) VALUES (?, ?, ?);
		INSERT INTO actresses (id, dmm_id, first_name, last_name, japanese_name) VALUES
			(?, ?, 'Legacy', 'Source', '旧ソース'),
			(?, 0, 'Missing', 'Alpha', '欠落A'),
			(?, 0, 'Missing', 'Beta', '欠落B'),
			(?, ?, 'Curated', 'Target', '正規対象');
		INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?), (?, ?), (?, ?);`,
		movieID, movieID, "Legacy Movie",
		sourceID, sourceDMMID, missingAID, missingBID, targetID, 81004,
		movieID, sourceID, movieID, missingAID, movieID, missingBID,
	)
	require.NoError(t, err)
	require.NoError(t, legacy.Close())

	migrated, err := New(config(legacyPath))
	require.NoError(t, err)
	defer func() { _ = migrated.Close() }()
	require.NoError(t, migrated.RunMigrationsOnStartup(ctx))
	migratedSQL, err := migrated.DB.DB()
	require.NoError(t, err)
	assertMigrated(migratedSQL)
	require.NoError(t, migrated.Close())

	backups, err := filepath.Glob(legacyPath + ".*.backup")
	require.NoError(t, err)
	require.Len(t, backups, 1)
	backupSQL, err := sql.Open("sqlite3", backups[0])
	require.NoError(t, err)
	defer func() { _ = backupSQL.Close() }()
	var oldVersion int64
	require.NoError(t, backupSQL.QueryRowContext(ctx, "SELECT MAX(version_id) FROM schema_migrations").Scan(&oldVersion))
	require.Equal(t, int64(15), oldVersion)
	require.Equal(t, int64(3), countFor(backupSQL, "SELECT COUNT(*) FROM movie_actresses WHERE movie_content_id = ?", movieID))
	require.Equal(t, int64(1), countFor(backupSQL, "SELECT COUNT(*) FROM actresses WHERE id = ? AND dmm_id = ?", sourceID, sourceDMMID))
	var creditTable string
	require.ErrorIs(t, backupSQL.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'movie_credits'").Scan(&creditTable), sql.ErrNoRows)
	require.NoError(t, backupSQL.Close())

	backupBytes, err := os.ReadFile(backups[0])
	require.NoError(t, err)
	restoredPath := filepath.Join(tmpDir, "restored.db")
	require.NoError(t, os.WriteFile(restoredPath, backupBytes, 0o600))
	restoredLegacySQL, err := sql.Open("sqlite3", restoredPath)
	require.NoError(t, err)
	defer func() { _ = restoredLegacySQL.Close() }()
	require.Equal(t, int64(3), countFor(restoredLegacySQL, "SELECT COUNT(*) FROM movie_actresses WHERE movie_content_id = ?", movieID))
	require.NoError(t, restoredLegacySQL.Close())

	restored, err := New(config(restoredPath))
	require.NoError(t, err)
	defer func() { _ = restored.Close() }()
	require.NoError(t, restored.RunMigrationsOnStartup(ctx))
	restoredSQL, err := restored.DB.DB()
	require.NoError(t, err)
	assertMigrated(restoredSQL)

	repos := restored.Repositories()
	sourceCredit, err := repos.MovieCreditRepo.FindByMovieAndActress(ctx, movieID, sourceID)
	require.NoError(t, err)
	require.True(t, sourceCredit.LegacyInferred)
	require.Equal(t, string(models.CreditOriginUser), sourceCredit.Origin)
	require.NoError(t, repos.MovieCreditRepo.ReassignCredit(ctx, sourceCredit, targetID))
	_, err = repos.MovieCreditRepo.FindByMovieAndActress(ctx, movieID, sourceID)
	require.Error(t, err)
	targetCredit, err := repos.MovieCreditRepo.FindByMovieAndActress(ctx, movieID, targetID)
	require.NoError(t, err)
	require.True(t, targetCredit.LegacyInferred)
	require.Equal(t, string(models.CreditOriginUser), targetCredit.Origin)
	var mappedTarget uint
	require.NoError(t, restoredSQL.QueryRowContext(ctx, "SELECT target_actress_id FROM movie_credit_reassignments WHERE movie_content_id = ? AND source_actress_id = ?", movieID, sourceID).Scan(&mappedTarget))
	require.Equal(t, targetID, mappedTarget)
	dirty, generation := readGeneration(restoredSQL)
	require.True(t, dirty)
	require.Equal(t, int64(1), generation)
	assertProjection(restoredSQL, []uint{targetID, missingAID, missingBID})
	assertForeignKeys(restoredSQL)

	missingCredit, err := repos.MovieCreditRepo.FindByMovieAndActress(ctx, movieID, missingAID)
	require.NoError(t, err)
	require.NoError(t, NewCollisionService(restored).SetCreditSuppressed(ctx, missingCredit.ID, true))
	dirty, generation = readGeneration(restoredSQL)
	require.True(t, dirty)
	require.Equal(t, int64(2), generation)
	assertProjection(restoredSQL, []uint{targetID, missingBID})
	assertForeignKeys(restoredSQL)
	require.NoError(t, restored.Close())

	reopened, err := New(config(restoredPath))
	require.NoError(t, err)
	defer func() { _ = reopened.Close() }()
	require.NoError(t, reopened.RunMigrationsOnStartup(ctx))
	reopenedSQL, err := reopened.DB.DB()
	require.NoError(t, err)
	movieRepo := NewMovieRepository(reopened)
	verifyReopened := func(expectedGeneration int64) {
		found, findErr := movieRepo.FindByID(ctx, movieID)
		require.NoError(t, findErr)
		require.Len(t, found.Credits, 3)
		byActress := make(map[uint]models.MovieCredit, len(found.Credits))
		for _, credit := range found.Credits {
			byActress[credit.ActressID] = credit
		}
		require.NotContains(t, byActress, sourceID)
		require.Equal(t, targetID, byActress[targetID].ActressID)
		require.Equal(t, string(models.CreditOriginUser), byActress[targetID].Origin)
		require.True(t, byActress[missingAID].Suppressed)
		actressIDs := make([]uint, 0, len(found.Actresses))
		for _, actress := range found.Actresses {
			actressIDs = append(actressIDs, actress.ID)
		}
		require.ElementsMatch(t, []uint{targetID, missingBID}, actressIDs)
		dirty, generation := readGeneration(reopenedSQL)
		require.True(t, dirty)
		require.Equal(t, expectedGeneration, generation)
		require.NoError(t, reopenedSQL.QueryRowContext(ctx, "SELECT target_actress_id FROM movie_credit_reassignments WHERE movie_content_id = ? AND source_actress_id = ?", movieID, sourceID).Scan(&mappedTarget))
		require.Equal(t, targetID, mappedTarget)
		assertProjection(reopenedSQL, []uint{targetID, missingBID})
		assertForeignKeys(reopenedSQL)
	}
	verifyReopened(2)

	scrape := func() {
		_, scrapeErr := movieRepo.Upsert(ctx, &models.Movie{
			ContentID: movieID,
			ID:        movieID,
			Title:     "Local fixture scrape",
			Credits: []models.MovieCredit{{
				CreditedName: "Recollected Source",
				Source:       "local-fixture",
				Origin:       string(models.CreditOriginScrape),
				Scraped: models.Actress{
					DMMID:     sourceDMMID,
					FirstName: "Legacy",
					LastName:  "Source",
				},
			}},
		})
		require.NoError(t, scrapeErr)
	}
	scrape()
	verifyReopened(3)
	scrape()
	verifyReopened(3)
}
