package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func seedVerifiedAliasCanonicalConflict(t *testing.T, db *DB) (models.Actress, models.Actress) {
	t.Helper()
	aliasOwner := models.Actress{JapaneseName: "Alias Owner", Verified: true, Origin: ActressOriginUser}
	canonicalOwner := models.Actress{JapaneseName: "Ｓｔａｇｅ　Ｎａｍｅ", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&aliasOwner).Error)
	require.NoError(t, db.Create(&canonicalOwner).Error)
	require.NoError(t, db.Create(&models.ActressAlias{AliasName: "Stage Name", CanonicalName: aliasOwner.JapaneseName}).Error)
	return aliasOwner, canonicalOwner
}

func TestVerifiedAliasAndCanonicalEvidenceIsUnified(t *testing.T) {
	t.Run("distinct owners create an ambiguity candidate", func(t *testing.T) {
		db := newCreditTestDB(t)
		aliasOwner, canonicalOwner := seedVerifiedAliasCanonicalConflict(t, db)

		resolved, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{JapaneseName: "stage name"})
		require.NoError(t, err)
		require.Equal(t, ResolutionAmbiguous, outcome)
		require.False(t, resolved.Verified)
		require.NotEqual(t, aliasOwner.ID, resolved.ID)
		require.NotEqual(t, canonicalOwner.ID, resolved.ID)
	})

	t.Run("overlapping evidence for one owner remains matched", func(t *testing.T) {
		db := newCreditTestDB(t)
		owner := models.Actress{JapaneseName: "Stage Name", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&owner).Error)
		require.NoError(t, db.Create(&models.ActressAlias{AliasName: "ＳＴＡＧＥ　ＮＡＭＥ", CanonicalName: owner.JapaneseName}).Error)

		resolved, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{JapaneseName: "stage name"})
		require.NoError(t, err)
		require.Equal(t, ResolutionMatched, outcome)
		require.Equal(t, owner.ID, resolved.ID)
	})
}

func TestVerifiedEvidenceAmbiguityPersistsAcrossMovieUpsertsAndRestart(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "verified-evidence.db")
	open := func() *DB {
		db, err := New(&Config{Type: "sqlite", DSN: dsn, LogLevel: "silent"})
		require.NoError(t, err)
		require.NoError(t, db.RunMigrationsOnStartup(ctx))
		return db
	}

	db := open()
	aliasOwner, canonicalOwner := seedVerifiedAliasCanonicalConflict(t, db)
	upsert := func(db *DB, movieID string) *models.Movie {
		t.Helper()
		movie := creditMovie(movieID, []models.MovieCredit{{
			CreditedJapaneseName: "Stage Name",
			Scraped:              models.Actress{JapaneseName: "Stage Name"},
		}})
		saved, err := NewMovieRepository(db).Upsert(ctx, movie)
		require.NoError(t, err)
		require.Len(t, saved.Credits, 1)
		require.Empty(t, saved.Actresses)
		return saved
	}

	first := upsert(db, "verified-union-one")
	candidateID := first.Credits[0].ActressID
	require.NotEqual(t, aliasOwner.ID, candidateID)
	require.NotEqual(t, canonicalOwner.ID, candidateID)
	firstAgain := upsert(db, "verified-union-one")
	require.Equal(t, candidateID, firstAgain.Credits[0].ActressID)
	require.NoError(t, db.Close())

	db = open()
	t.Cleanup(func() { _ = db.Close() })
	other := upsert(db, "verified-union-two")
	require.Equal(t, candidateID, other.Credits[0].ActressID)
	for _, movieID := range []string{"verified-union-one", "verified-union-two"} {
		var collisionCount int64
		require.NoError(t, db.Model(&models.CreditCollision{}).Where("movie_content_id = ? AND field = ? AND status = ?", movieID, models.CreditFieldIdentityLink, models.CollisionStatusOpen).Count(&collisionCount).Error)
		require.EqualValues(t, 1, collisionCount)
	}
	for _, ownerID := range []uint{aliasOwner.ID, canonicalOwner.ID} {
		var creditCount int64
		require.NoError(t, db.Model(&models.MovieCredit{}).Where("actress_id = ?", ownerID).Count(&creditCount).Error)
		require.Zero(t, creditCount)
	}

	reassignmentTarget := models.Actress{JapaneseName: "Reviewed Identity", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&reassignmentTarget).Error)
	require.NoError(t, NewMovieCreditRepository(db).ReassignCredit(ctx, &other.Credits[0], reassignmentTarget.ID))
	reassigned, err := NewMovieRepository(db).Upsert(ctx, creditMovie("verified-union-two", []models.MovieCredit{{
		CreditedJapaneseName: "Stage Name", Scraped: models.Actress{JapaneseName: "Stage Name"},
	}}))
	require.NoError(t, err)
	require.Equal(t, reassignmentTarget.ID, reassigned.Credits[0].ActressID)

	require.NoError(t, NewActressRepository(db).PromoteCandidate(ctx, candidateID, "Resolved", "Identity", "", ""))
	firstPersisted, err := NewMovieRepository(db).FindByContentID(ctx, "verified-union-one")
	require.NoError(t, err)
	require.Len(t, firstPersisted.Actresses, 1)
	require.Equal(t, candidateID, firstPersisted.Actresses[0].ID)
	secondPersisted, err := NewMovieRepository(db).FindByContentID(ctx, "verified-union-two")
	require.NoError(t, err)
	require.Len(t, secondPersisted.Actresses, 1)
	require.Equal(t, reassignmentTarget.ID, secondPersisted.Actresses[0].ID)
}

func TestActressDeleteRemovesTranslationsAndIdentityGraphWithForeignKeysOnOrOff(t *testing.T) {
	for _, foreignKeys := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreign keys off", true: "foreign keys on"}[foreignKeys], func(t *testing.T) {
			dsn := filepath.Join(t.TempDir(), "delete.db") + map[bool]string{false: "?_foreign_keys=0", true: "?_foreign_keys=1"}[foreignKeys]
			db, err := New(&Config{Type: "sqlite", DSN: dsn, LogLevel: "silent"})
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))

			actress := models.Actress{JapaneseName: "Delete Graph", Verified: true, Origin: ActressOriginUser}
			target := models.Actress{JapaneseName: "Delete Target", Verified: true, Origin: ActressOriginUser}
			require.NoError(t, db.Create(&actress).Error)
			require.NoError(t, db.Create(&target).Error)
			require.NoError(t, db.Create(&models.ActressTranslation{ActressID: actress.ID, Language: "en", DisplayName: "Delete Graph"}).Error)
			movie := models.Movie{ContentID: "delete-graph", ID: "delete-graph", RenderGeneration: 3}
			require.NoError(t, db.Create(&movie).Error)
			credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, Origin: string(models.CreditOriginUser)}
			require.NoError(t, db.Create(&credit).Error)
			require.NoError(t, db.Create(&models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldIdentityLink, Status: models.CollisionStatusOpen}).Error)
			require.NoError(t, db.Create(&models.MovieCreditReassignment{MovieContentID: movie.ContentID, SourceActressID: actress.ID, TargetActressID: target.ID}).Error)
			require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", movie.ContentID, actress.ID).Error)

			require.NoError(t, NewActressRepository(db).Delete(t.Context(), actress.ID))
			for table, predicate := range map[string]struct {
				where string
				value any
			}{
				"actresses": {"id = ?", actress.ID}, "actress_translations": {"actress_id = ?", actress.ID},
				"movie_credits": {"actress_id = ?", actress.ID}, "credit_collisions": {"credit_id = ?", credit.ID},
				"movie_credit_reassignments": {"source_actress_id = ?", actress.ID}, "movie_actresses": {"actress_id = ?", actress.ID},
			} {
				var count int64
				require.NoError(t, db.Table(table).Where(predicate.where, predicate.value).Count(&count).Error)
				require.Zero(t, count, table)
			}
			var storedMovie models.Movie
			require.NoError(t, db.First(&storedMovie, "content_id = ?", movie.ContentID).Error)
			require.True(t, storedMovie.RenderDirty)
			require.Equal(t, int64(4), storedMovie.RenderGeneration)
		})
	}
}

func TestActressDeleteTranslationFailureRollsBackEntireIdentityGraph(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "delete-rollback.db") + "?_foreign_keys=0"
	db, err := New(&Config{Type: "sqlite", DSN: dsn, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	actress := models.Actress{JapaneseName: "Rollback Graph", Verified: true, Origin: ActressOriginUser}
	target := models.Actress{JapaneseName: "Rollback Target", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actress).Error)
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&models.ActressTranslation{ActressID: actress.ID, Language: "en", DisplayName: "Rollback"}).Error)
	movie := models.Movie{ContentID: "delete-rollback-translation", ID: "delete-rollback-translation", RenderGeneration: 9}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, Origin: string(models.CreditOriginUser)}
	require.NoError(t, db.Create(&credit).Error)
	require.NoError(t, db.Create(&models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldIdentityLink, Status: models.CollisionStatusOpen}).Error)
	require.NoError(t, db.Create(&models.MovieCreditReassignment{MovieContentID: movie.ContentID, SourceActressID: actress.ID, TargetActressID: target.ID}).Error)
	require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", movie.ContentID, actress.ID).Error)
	require.NoError(t, db.Exec("CREATE TRIGGER fail_explicit_translation_delete BEFORE DELETE ON actress_translations BEGIN SELECT RAISE(ABORT, 'translation delete failure'); END").Error)

	require.Error(t, NewActressRepository(db).Delete(t.Context(), actress.ID))
	for table, predicate := range map[string]struct {
		where string
		value any
	}{
		"actresses": {"id = ?", actress.ID}, "actress_translations": {"actress_id = ?", actress.ID},
		"movie_credits": {"actress_id = ?", actress.ID}, "credit_collisions": {"credit_id = ?", credit.ID},
		"movie_credit_reassignments": {"source_actress_id = ?", actress.ID}, "movie_actresses": {"actress_id = ?", actress.ID},
	} {
		var count int64
		require.NoError(t, db.Table(table).Where(predicate.where, predicate.value).Count(&count).Error)
		require.EqualValues(t, 1, count, table)
	}
	var storedMovie models.Movie
	require.NoError(t, db.First(&storedMovie, "content_id = ?", movie.ContentID).Error)
	require.False(t, storedMovie.RenderDirty)
	require.Equal(t, int64(9), storedMovie.RenderGeneration)
}
