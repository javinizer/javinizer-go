package database

import (
	"context"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func updatePolicyDB(t *testing.T) *DB {
	t.Helper()
	db := setupBaseRepoTestDB(t)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

func rejectPolicyUpdate(t *testing.T, db *DB, table string) {
	t.Helper()
	// All callers use fixed table names and an isolated database. The policy
	// rejects updates, not reads, so the original record can still be verified.
	require.NoError(t, db.Exec(fmt.Sprintf("CREATE TRIGGER reject_policy_update BEFORE UPDATE ON %s BEGIN SELECT RAISE(ABORT, 'record is protected'); END", table)).Error)
}

func TestReplacementUpsertPolicyPreservesExistingRecord(t *testing.T) {
	t.Run("genre", func(t *testing.T) {
		db := updatePolicyDB(t)
		repo := NewGenreReplacementRepository(db)
		ctx := context.Background()
		require.NoError(t, repo.Upsert(ctx, &models.GenreReplacement{Original: "protected", Replacement: "kept"}))
		before, err := repo.FindByOriginal(ctx, "protected")
		require.NoError(t, err)
		rejectPolicyUpdate(t, db, "genre_replacements")
		err = repo.Upsert(ctx, &models.GenreReplacement{Original: "protected", Replacement: "rejected"})
		require.EqualError(t, err, "update genre replacement protected: record is protected")
		after, err := repo.FindByOriginal(ctx, "protected")
		require.NoError(t, err)
		require.Equal(t, before, after)
	})
	t.Run("word", func(t *testing.T) {
		db := updatePolicyDB(t)
		repo := NewWordReplacementRepository(db)
		ctx := context.Background()
		require.NoError(t, repo.Upsert(ctx, &models.WordReplacement{Original: "protected", Replacement: "kept", MatchMode: models.MatchModeWildcard}))
		before, err := repo.FindByOriginal(ctx, "protected")
		require.NoError(t, err)
		rejectPolicyUpdate(t, db, "word_replacements")
		err = repo.Upsert(ctx, &models.WordReplacement{Original: "protected", Replacement: "rejected", MatchMode: models.MatchModeLiteral})
		require.EqualError(t, err, "update word replacement protected: record is protected")
		after, err := repo.FindByOriginal(ctx, "protected")
		require.NoError(t, err)
		require.Equal(t, before, after)
	})
}

func TestTokenRegenerationPolicyPreservesHashAndPrefix(t *testing.T) {
	db := updatePolicyDB(t)
	repo := NewApiTokenRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, &models.ApiToken{ID: "protected", Name: "fixture", TokenHash: "1111111111111111111111111111111111111111111111111111111111111111", TokenPrefix: "11111111"}))
	before, err := repo.FindByID(ctx, "protected")
	require.NoError(t, err)
	rejectPolicyUpdate(t, db, "api_tokens")
	result, err := repo.Regenerate(ctx, "protected", "2222222222222222222222222222222222222222222222222222222222222222", "22222222")
	require.Nil(t, result)
	require.EqualError(t, err, "regenerate api token protected: record is protected")
	after, err := repo.FindByID(ctx, "protected")
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestTranslationUpsertPolicyPreservesExistingRecord(t *testing.T) {
	t.Run("movie", func(t *testing.T) {
		db := updatePolicyDB(t)
		ctx := context.Background()
		require.NoError(t, NewMovieRepository(db).Create(ctx, createTestMovie("POLICY-001")))
		repo := newMovieTranslationRepository(db)
		require.NoError(t, repo.Upsert(ctx, &models.MovieTranslation{MovieID: "POLICY-001", Language: "en", Title: "kept", Description: "original description"}))
		before, err := repo.FindByMovieAndLanguage(ctx, "POLICY-001", "en")
		require.NoError(t, err)
		rejectPolicyUpdate(t, db, "movie_translations")
		err = repo.Upsert(ctx, &models.MovieTranslation{MovieID: "POLICY-001", Language: "en", Title: "rejected"})
		require.EqualError(t, err, "update translation POLICY-001/en: record is protected")
		after, err := repo.FindByMovieAndLanguage(ctx, "POLICY-001", "en")
		require.NoError(t, err)
		require.Equal(t, before, after)
	})
	t.Run("genre", func(t *testing.T) {
		db := updatePolicyDB(t)
		ctx := context.Background()
		genre, err := newGenreRepository(db).FindOrCreate(ctx, "Policy genre")
		require.NoError(t, err)
		repo := newGenreTranslationRepository(db)
		require.NoError(t, repo.Upsert(ctx, &models.GenreTranslation{GenreID: genre.ID, Language: "en", Name: "kept"}))
		before, err := repo.FindByGenreAndLanguage(ctx, genre.ID, "en")
		require.NoError(t, err)
		rejectPolicyUpdate(t, db, "genre_translations")
		err = repo.Upsert(ctx, &models.GenreTranslation{GenreID: genre.ID, Language: "en", Name: "rejected"})
		require.EqualError(t, err, fmt.Sprintf("update genre translation %d/en: record is protected", genre.ID))
		after, err := repo.FindByGenreAndLanguage(ctx, genre.ID, "en")
		require.NoError(t, err)
		require.Equal(t, before, after)
	})
	t.Run("actress", func(t *testing.T) {
		db := updatePolicyDB(t)
		ctx := context.Background()
		actress := &models.Actress{DMMID: 10001, JapaneseName: "Policy actress"}
		require.NoError(t, NewActressRepository(db).Create(ctx, actress))
		repo := newActressTranslationRepository(db)
		require.NoError(t, repo.Upsert(ctx, &models.ActressTranslation{ActressID: actress.ID, Language: "en", DisplayName: "kept"}))
		before, err := repo.FindByActressAndLanguage(ctx, actress.ID, "en")
		require.NoError(t, err)
		rejectPolicyUpdate(t, db, "actress_translations")
		err = repo.Upsert(ctx, &models.ActressTranslation{ActressID: actress.ID, Language: "en", DisplayName: "rejected"})
		require.EqualError(t, err, fmt.Sprintf("update actress translation %d/en: record is protected", actress.ID))
		after, err := repo.FindByActressAndLanguage(ctx, actress.ID, "en")
		require.NoError(t, err)
		require.Equal(t, before, after)
	})
}
