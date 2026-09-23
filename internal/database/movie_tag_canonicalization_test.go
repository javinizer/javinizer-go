package database

import (
	"context"
	"testing"

	dbmigrations "github.com/javinizer/javinizer-go/internal/database/migrations"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
)

func TestMovieTagRepositoryCanonicalizesLegacyKeysAcrossReadsAndMutations(t *testing.T) {
	db := newDatabaseTestDB(t)
	movie := models.Movie{ContentID: "opaque-content", ID: "IPX-535"}
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Create(&models.MovieTag{MovieID: movie.ContentID, Tag: "duplicate"}).Error)
	require.NoError(t, db.Create(&models.MovieTag{MovieID: movie.ID, Tag: "duplicate"}).Error)
	require.NoError(t, db.Create(&models.MovieTag{MovieID: movie.ID, Tag: "legacy-only"}).Error)
	repo := NewMovieTagRepository(db)

	for _, key := range []string{movie.ContentID, movie.ID} {
		tags, err := repo.GetTagsForMovie(t.Context(), key)
		require.NoError(t, err)
		require.Equal(t, []string{"duplicate", "legacy-only"}, tags)
	}
	movies, err := repo.GetMoviesWithTag(t.Context(), "duplicate")
	require.NoError(t, err)
	require.Equal(t, []string{movie.ContentID}, movies)
	page, err := repo.ListTagsPaginated(t.Context(), 10, 0)
	require.NoError(t, err)
	require.Len(t, page, 2)
	for _, row := range page {
		require.Equal(t, movie.ContentID, row.MovieID)
	}
	all, err := repo.ListAll(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"duplicate", "legacy-only"}, all[movie.ContentID])
	chunked, err := repo.ListAllChunked(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, all, chunked)

	require.NoError(t, repo.RemoveTag(t.Context(), movie.ID, "legacy-only"))
	var legacyCount, canonicalCount int64
	require.NoError(t, db.Model(&models.MovieTag{}).Where("movie_id = ?", movie.ID).Count(&legacyCount).Error)
	require.NoError(t, db.Model(&models.MovieTag{}).Where("movie_id = ?", movie.ContentID).Count(&canonicalCount).Error)
	require.Zero(t, legacyCount)
	require.EqualValues(t, 1, canonicalCount)
	dirty, generation := tagRenderState(t, db, movie.ContentID)
	require.True(t, dirty)
	require.EqualValues(t, 1, generation)
}

func TestMovieTagRepositoryBulkReadsPreserveAmbiguousDisplayKeys(t *testing.T) {
	db := newDatabaseTestDB(t)
	require.NoError(t, db.Create(&models.Movie{ContentID: "ambiguous-a", ID: "DUPLICATE"}).Error)
	require.NoError(t, db.Create(&models.Movie{ContentID: "ambiguous-b", ID: "DUPLICATE"}).Error)
	require.NoError(t, db.Create(&models.MovieTag{MovieID: "DUPLICATE", Tag: "legacy"}).Error)
	require.NoError(t, db.Create(&models.MovieTag{MovieID: "DUPLICATE", Tag: "collision"}).Error)
	require.NoError(t, db.Create(&models.MovieTag{MovieID: "ambiguous-a", Tag: "collision"}).Error)
	repo := NewMovieTagRepository(db)

	movies, err := repo.GetMoviesWithTag(t.Context(), "legacy")
	require.NoError(t, err)
	require.Equal(t, []string{"DUPLICATE"}, movies)

	page, err := repo.ListTagsPaginated(t.Context(), 10, 0)
	require.NoError(t, err)
	require.Len(t, page, 3)
	require.Equal(t, []string{"DUPLICATE", "DUPLICATE", "ambiguous-a"}, []string{page[0].MovieID, page[1].MovieID, page[2].MovieID})

	all, err := repo.ListAll(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"collision", "legacy"}, all["DUPLICATE"])
	require.Equal(t, []string{"collision"}, all["ambiguous-a"])

	chunked, err := repo.ListAllChunked(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, all, chunked)
}

func TestMovieTagRepositoryExactContentIDPrecedesDisplayIDCollision(t *testing.T) {
	db := newDatabaseTestDB(t)
	exact := models.Movie{ContentID: "shared-key", ID: "EXACT-DISPLAY"}
	colliding := models.Movie{ContentID: "other-content", ID: "shared-key"}
	require.NoError(t, db.Create(&exact).Error)
	require.NoError(t, db.Create(&colliding).Error)
	repo := NewMovieTagRepository(db)
	require.NoError(t, repo.AddTag(t.Context(), "shared-key", "winner"))

	var row models.MovieTag
	require.NoError(t, db.Where("tag = ?", "winner").Take(&row).Error)
	require.Equal(t, exact.ContentID, row.MovieID)
	_, exactGeneration := tagRenderState(t, db, exact.ContentID)
	_, otherGeneration := tagRenderState(t, db, colliding.ContentID)
	require.EqualValues(t, 1, exactGeneration)
	require.Zero(t, otherGeneration)
}

func TestMovieTagCanonicalizationMigrationDeduplicatesAndCycles(t *testing.T) {
	db := newDatabaseTestDB(t)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, sqlDB, dbmigrations.Filesystem(), goose.WithTableName(schemaMigrationsTable), goose.WithDisableGlobalRegistry(true))
	require.NoError(t, err)
	ctx := context.Background()
	_, err = provider.DownTo(ctx, 20)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx, `
		INSERT INTO movies (content_id,id,title) VALUES
		('canonical','LEGACY','one'),
		('DISPLAY','COLLISION','two'),
		('empty-display','','three');
		INSERT INTO movie_tags (id,movie_id,tag) VALUES
		(21001,'LEGACY','duplicate'),
		(21002,'canonical','duplicate'),
		(21003,'DISPLAY','exact-wins'),
		(21004,'orphan','orphan'),
		(21005,'','empty');`)
	require.NoError(t, err)
	_, err = provider.Up(ctx)
	require.NoError(t, err)

	type row struct {
		id           uint
		movieID, tag string
	}
	rows, err := sqlDB.QueryContext(ctx, "SELECT id,movie_id,tag FROM movie_tags ORDER BY id")
	require.NoError(t, err)
	var got []row
	for rows.Next() {
		var current row
		require.NoError(t, rows.Scan(&current.id, &current.movieID, &current.tag))
		got = append(got, current)
	}
	require.NoError(t, rows.Close())
	require.Equal(t, []row{{21001, "canonical", "duplicate"}, {21003, "DISPLAY", "exact-wins"}, {21004, "orphan", "orphan"}, {21005, "", "empty"}}, got)
	_, err = sqlDB.ExecContext(ctx, "INSERT INTO movie_tags (movie_id,tag) VALUES ('canonical','duplicate')")
	require.Error(t, err)
	_, err = provider.DownTo(ctx, 20)
	require.NoError(t, err)
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	var count int
	require.NoError(t, sqlDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM movie_tags").Scan(&count))
	require.Equal(t, 4, count)
}

func TestMovieTagCanonicalizationMigrationPreservesAmbiguousDisplayKeys(t *testing.T) {
	db := newDatabaseTestDB(t)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, sqlDB, dbmigrations.Filesystem(), goose.WithTableName(schemaMigrationsTable), goose.WithDisableGlobalRegistry(true))
	require.NoError(t, err)
	ctx := context.Background()
	_, err = provider.DownTo(ctx, 20)
	require.NoError(t, err)
	_, err = sqlDB.ExecContext(ctx, `
		INSERT INTO movies (content_id,id,title) VALUES
		('ambiguous-a','DUPLICATE','one'),
		('ambiguous-b','DUPLICATE','two'),
		('unique-content','UNIQUE','three'),
		('exact-key','EXACT','four'),
		('other-content','exact-key','five');
		INSERT INTO movie_tags (id,movie_id,tag) VALUES
		(21101,'DUPLICATE','collision'),
		(21102,'ambiguous-a','collision'),
		(21103,'DUPLICATE','legacy'),
		(21104,'UNIQUE','unique'),
		(21105,'exact-key','exact');`)
	require.NoError(t, err)

	assertRows := func() {
		t.Helper()
		rows, queryErr := sqlDB.QueryContext(ctx, "SELECT movie_id,tag FROM movie_tags ORDER BY id")
		require.NoError(t, queryErr)
		defer rows.Close()
		var got [][2]string
		for rows.Next() {
			var movieID, tag string
			require.NoError(t, rows.Scan(&movieID, &tag))
			got = append(got, [2]string{movieID, tag})
		}
		require.NoError(t, rows.Err())
		require.Equal(t, [][2]string{
			{"DUPLICATE", "collision"},
			{"ambiguous-a", "collision"},
			{"DUPLICATE", "legacy"},
			{"unique-content", "unique"},
			{"exact-key", "exact"},
		}, got)
	}

	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assertRows()
	_, err = provider.DownTo(ctx, 20)
	require.NoError(t, err)
	assertRows()
	_, err = provider.Up(ctx)
	require.NoError(t, err)
	assertRows()
}

func TestMovieTagLegacyNormalizationRollsBackWithInvalidation(t *testing.T) {
	db := newDatabaseTestDB(t)
	movie := models.Movie{ContentID: "rollback-content", ID: "ROLLBACK-DISPLAY"}
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Create(&models.MovieTag{MovieID: movie.ID, Tag: "legacy"}).Error)
	require.NoError(t, db.Exec("CREATE TRIGGER fail_legacy_tag_generation BEFORE UPDATE OF render_generation ON movies BEGIN SELECT RAISE(ABORT, 'generation failure'); END").Error)
	repo := NewMovieTagRepository(db)
	require.Error(t, repo.RemoveTag(t.Context(), movie.ID, "legacy"))
	var row models.MovieTag
	require.NoError(t, db.Where("movie_id = ? AND tag = ?", movie.ID, "legacy").Take(&row).Error)
	_, generation := tagRenderState(t, db, movie.ContentID)
	require.Zero(t, generation)
}

func TestMovieTagAddExistingLegacyTagCanonicalizesWithoutConflict(t *testing.T) {
	db := newDatabaseTestDB(t)
	movie := models.Movie{ContentID: "legacy-add-content", ID: "LEGACY-ADD"}
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Create(&models.MovieTag{MovieID: movie.ID, Tag: "existing"}).Error)
	repo := NewMovieTagRepository(db)
	require.NoError(t, repo.AddTag(t.Context(), movie.ID, "existing"))
	var rows []models.MovieTag
	require.NoError(t, db.Where("tag = ?", "existing").Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, movie.ContentID, rows[0].MovieID)
	_, generation := tagRenderState(t, db, movie.ContentID)
	require.Zero(t, generation)
}

func TestMovieTagCanonicalizationFailurePaths(t *testing.T) {
	t.Run("empty key remains orphan", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		key, err := resolveMovieTagKey(db.DB, "")
		require.NoError(t, err)
		require.Equal(t, movieTagKey{canonical: ""}, key)
	})
	t.Run("ambiguous display ID is rejected", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		require.NoError(t, db.Create(&models.Movie{ContentID: "ambiguous-a", ID: "AMBIGUOUS"}).Error)
		require.NoError(t, db.Create(&models.Movie{ContentID: "ambiguous-b", ID: "AMBIGUOUS"}).Error)
		_, err := NewMovieTagRepository(db).GetTagsForMovie(t.Context(), "AMBIGUOUS")
		require.ErrorContains(t, err, "ambiguous")
	})
	t.Run("resolver query error", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		require.NoError(t, db.Exec("DROP TABLE movies").Error)
		_, err := resolveMovieTagKey(db.DB, "missing-schema")
		require.Error(t, err)
	})
	for _, operation := range []struct {
		name string
		run  func(*MovieTagRepository) error
	}{
		{name: "add resolve", run: func(repo *MovieTagRepository) error { return repo.AddTag(t.Context(), "key", "tag") }},
		{name: "remove resolve", run: func(repo *MovieTagRepository) error { return repo.RemoveTag(t.Context(), "key", "tag") }},
		{name: "read resolve", run: func(repo *MovieTagRepository) error { _, err := repo.GetTagsForMovie(t.Context(), "key"); return err }},
	} {
		t.Run(operation.name, func(t *testing.T) {
			db := newDatabaseTestDB(t)
			require.NoError(t, db.Exec("DROP TABLE movies").Error)
			require.Error(t, operation.run(NewMovieTagRepository(db)))
		})
	}
	t.Run("add legacy lookup", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		movie := models.Movie{ContentID: "lookup-error-content", ID: "LOOKUP-ERROR"}
		require.NoError(t, db.Create(&movie).Error)
		require.NoError(t, db.Exec("DROP TABLE movie_tags").Error)
		require.Error(t, NewMovieTagRepository(db).AddTag(t.Context(), movie.ID, "tag"))
	})
	for _, trigger := range []struct {
		name          string
		sql           string
		seedCanonical bool
	}{
		{name: "delete", seedCanonical: true, sql: "CREATE TRIGGER fail_legacy_delete BEFORE DELETE ON movie_tags BEGIN SELECT RAISE(ABORT, 'delete failure'); END"},
		{name: "update", sql: "CREATE TRIGGER fail_legacy_update BEFORE UPDATE OF movie_id ON movie_tags BEGIN SELECT RAISE(ABORT, 'update failure'); END"},
	} {
		t.Run("normalize "+trigger.name, func(t *testing.T) {
			db := newDatabaseTestDB(t)
			movie := models.Movie{ContentID: "normalize-" + trigger.name, ID: "NORMALIZE-" + trigger.name}
			require.NoError(t, db.Create(&movie).Error)
			require.NoError(t, db.Create(&models.MovieTag{MovieID: movie.ID, Tag: "tag"}).Error)
			if trigger.seedCanonical {
				require.NoError(t, db.Create(&models.MovieTag{MovieID: movie.ContentID, Tag: "tag"}).Error)
			}
			require.NoError(t, db.Exec(trigger.sql).Error)
			require.Error(t, NewMovieTagRepository(db).RemoveTag(t.Context(), movie.ID, "missing"))
		})
	}
	t.Run("invalidation finds no row", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		movie := models.Movie{ContentID: "vanishing-content", ID: "VANISHING"}
		require.NoError(t, db.Create(&movie).Error)
		require.NoError(t, db.Exec("CREATE TRIGGER vanish_before_tag_invalidation BEFORE UPDATE OF render_generation ON movies BEGIN DELETE FROM movies WHERE content_id = OLD.content_id; SELECT RAISE(IGNORE); END").Error)
		repo := NewMovieTagRepository(db)
		require.Error(t, repo.AddTag(t.Context(), movie.ID, "tag"))
		var count int64
		require.NoError(t, db.Model(&models.MovieTag{}).Where("tag = ?", "tag").Count(&count).Error)
		require.Zero(t, count)
		require.NoError(t, db.First(&models.Movie{}, "content_id = ?", movie.ContentID).Error)
	})
}
