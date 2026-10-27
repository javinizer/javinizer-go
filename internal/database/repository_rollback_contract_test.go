package database

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Keep TEMP triggers on the same SQLite connection used by the public calls.
func newRollbackContractDB(t *testing.T) *DB {
	t.Helper()
	db := newCreditTestDB(t)
	pool, err := db.DB.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	return db
}

func TestAliasGroupQueryFailuresReturnNoPartialGroup(t *testing.T) {
	for _, tc := range []struct {
		name, input, column, key string
	}{
		{"canonical lookup", "Canonical", "canonical_name_key", "canonical"},
		{"alias lookup", "Alias", "alias_name_key", "alias"},
		{"resolved canonical lookup", "Alias", "canonical_name_key", "canonical"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newRollbackContractDB(t)
			repo := NewActressAliasRepository(db)
			require.NoError(t, repo.Create(t.Context(), &models.ActressAlias{AliasName: "Alias", CanonicalName: "Canonical"}))
			before := previewDatabaseState(t, db)
			fault := errors.New("alias group lookup unavailable")
			fired := 0
			const hook = "test:alias_group_lookup_failure"
			require.NoError(t, db.Callback().Query().After("gorm:query").Register(hook, func(tx *gorm.DB) {
				if tx.Statement.Table == "actress_aliases" && strings.Contains(tx.Statement.SQL.String(), tc.column+" = ?") &&
					len(tx.Statement.Vars) > 0 && tx.Statement.Vars[0] == tc.key {
					fired++
					_ = tx.AddError(fault)
				}
			}))
			t.Cleanup(func() { _ = db.Callback().Query().Remove(hook) })

			group, err := repo.GetAliasGroup(t.Context(), tc.input)
			require.ErrorIs(t, err, fault)
			require.Equal(t, AliasGroup{}, group, "a failed lookup must not expose a partial canonical or names list")
			require.Equal(t, 1, fired)
			require.NoError(t, db.Callback().Query().Remove(hook))
			require.Equal(t, before, previewDatabaseState(t, db))
			group, err = repo.GetAliasGroup(t.Context(), tc.input)
			require.NoError(t, err)
			require.Equal(t, AliasGroup{Canonical: "Canonical", Names: []string{"Canonical", "Alias"}}, group)
		})
	}
}

func requireTriggerFailure(t *testing.T, err error, message string) {
	t.Helper()
	var sqliteErr sqlite3.Error
	require.ErrorAs(t, err, &sqliteErr)
	require.Equal(t, sqlite3.ErrConstraintTrigger, sqliteErr.ExtendedCode)
	require.EqualError(t, sqliteErr, message)
}

func TestActressMergeAssociationFailuresRollBackIdentityAndCredits(t *testing.T) {
	for _, stage := range []string{"movie preload", "target association insert"} {
		t.Run(stage, func(t *testing.T) {
			db := newRollbackContractDB(t)
			repo := NewActressRepository(db)
			target := models.Actress{DMMID: 730021, FirstName: "Target", Aliases: "Target alias", Verified: true, Origin: ActressOriginUser}
			source := models.Actress{DMMID: 730022, FirstName: "Source", Aliases: "Source alias", Verified: true, Origin: ActressOriginUser}
			other := models.Actress{FirstName: "Bystander", Verified: true, Origin: ActressOriginUser}
			for _, actress := range []*models.Actress{&target, &source, &other} {
				require.NoError(t, repo.Create(t.Context(), actress))
			}
			aliasRepo := NewActressAliasRepository(db)
			require.NoError(t, aliasRepo.Create(t.Context(), &models.ActressAlias{AliasName: "Target alias", CanonicalName: "Target"}))
			require.NoError(t, aliasRepo.Create(t.Context(), &models.ActressAlias{AliasName: "Source alias", CanonicalName: "Source"}))
			movie := models.Movie{ContentID: "merge-rollback", ID: "MERGE-ROLLBACK", Title: "Preserved", Actresses: []models.Actress{source, other}, RenderGeneration: 7}
			require.NoError(t, db.Create(&movie).Error)
			require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: source.ID, CreditedName: "Source credit", OverrideName: "Local credit", UserOverride: true, Origin: string(models.CreditOriginUser)}).Error)
			require.NoError(t, db.Create(&models.ActressTranslation{ActressID: source.ID, Language: "en", DisplayName: "Source translation"}).Error)
			before := previewDatabaseState(t, db)

			fault := errors.New("movie association preload unavailable")
			fired := 0
			const hook = "test:merge_movie_preload_failure"
			const trigger = "reject_merge_target_association"
			if stage == "movie preload" {
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register(hook, func(tx *gorm.DB) {
					if _, loadingMovies := tx.Statement.Dest.(*[]models.Movie); loadingMovies && tx.Statement.Table == "movies" {
						if _, ok := tx.Statement.Preloads["Actresses"]; ok {
							fired++
							_ = tx.AddError(fault)
						}
					}
				}))
				t.Cleanup(func() { _ = db.Callback().Query().Remove(hook) })
			} else {
				require.NoError(t, db.Exec(fmt.Sprintf(`CREATE TEMP TRIGGER %s BEFORE INSERT ON movie_actresses
WHEN NEW.movie_content_id = 'merge-rollback' AND NEW.actress_id = %d
BEGIN SELECT RAISE(ABORT, 'target association rejected'); END`, trigger, target.ID)).Error)
				t.Cleanup(func() { _ = db.Exec("DROP TRIGGER IF EXISTS " + trigger).Error })
			}

			result, err := repo.Merge(t.Context(), target.ID, source.ID, map[string]string{"first_name": MergeResolutionSource})
			require.Nil(t, result)
			require.ErrorContains(t, err, "actress movie associations")
			if stage == "movie preload" {
				require.ErrorIs(t, err, fault)
				require.Equal(t, 1, fired)
				require.NoError(t, db.Callback().Query().Remove(hook))
			} else {
				requireTriggerFailure(t, err, "target association rejected")
				require.NoError(t, db.Exec("DROP TRIGGER "+trigger).Error)
			}
			// Includes both complete identities, canonical/alias indexes, credits,
			// translations, movie joins, and render generation/timestamps.
			require.Equal(t, before, previewDatabaseState(t, db))
			result, err = repo.Merge(t.Context(), target.ID, source.ID, map[string]string{"first_name": MergeResolutionSource})
			require.NoError(t, err, "removing the scoped fault allows the same public operation to finish")
			require.Equal(t, 1, result.UpdatedMovies)
			_, err = repo.FindByID(t.Context(), source.ID)
			require.ErrorIs(t, err, ErrNotFound)
		})
	}
}

func TestMovieDeleteFailuresRollBackAllAssociatedRows(t *testing.T) {
	for _, tc := range []struct{ name, table, key, context string }{
		{"genre clear", "movie_genres", "movie_content_id", "clear genres for movie"},
		{"final movie delete", "movies", "content_id", "delete movie"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newRollbackContractDB(t)
			actress := models.Actress{FirstName: "Preserved", Verified: true, Origin: ActressOriginUser}
			former := models.Actress{FirstName: "Former assignment", Verified: true, Origin: ActressOriginUser}
			genre := models.Genre{Name: "Preserved genre"}
			require.NoError(t, db.Create(&actress).Error)
			require.NoError(t, db.Create(&former).Error)
			require.NoError(t, db.Create(&genre).Error)
			movie := models.Movie{ContentID: "delete-rollback", ID: "DELETE-ROLLBACK", Title: "Preserved movie", Actresses: []models.Actress{actress}, Genres: []models.Genre{genre}, RenderGeneration: 9}
			require.NoError(t, db.Create(&movie).Error)
			credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, CreditedName: "Preserved credit", Origin: string(models.CreditOriginUser)}
			require.NoError(t, db.Create(&credit).Error)
			require.NoError(t, db.Create(&models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldCreditedName, ReportedValue: credit.CreditedName, CanonicalValue: actress.FirstName, Status: models.CollisionStatusOpen, Occurrences: 1, SourcesSeen: "fixture"}).Error)
			require.NoError(t, db.Create(&models.MovieCreditReassignment{MovieContentID: movie.ContentID, SourceActressID: former.ID, TargetActressID: actress.ID}).Error)
			require.NoError(t, db.Create(&models.MovieTranslation{MovieID: movie.ContentID, Language: "en", Title: "Preserved translation"}).Error)
			require.NoError(t, db.Create(&models.MovieTag{MovieID: movie.ContentID, Tag: "Preserved tag"}).Error)
			before := previewDatabaseState(t, db)
			require.NoError(t, db.Exec(fmt.Sprintf(`CREATE TEMP TRIGGER reject_movie_delete BEFORE DELETE ON %s
WHEN OLD.%s = 'delete-rollback' BEGIN SELECT RAISE(ABORT, 'movie delete rejected'); END`, tc.table, tc.key)).Error)
			t.Cleanup(func() { _ = db.Exec("DROP TRIGGER IF EXISTS reject_movie_delete").Error })

			repo := NewMovieRepository(db)
			err := repo.Delete(t.Context(), movie.ID)
			require.ErrorContains(t, err, tc.context)
			requireTriggerFailure(t, err, "movie delete rejected")
			require.Equal(t, before, previewDatabaseState(t, db), "earlier association and metadata deletes must roll back")
			require.NoError(t, db.Exec("DROP TRIGGER reject_movie_delete").Error)
			require.NoError(t, repo.Delete(t.Context(), movie.ID))
			for _, table := range []string{"movies", "movie_actresses", "movie_genres", "movie_credits", "credit_collisions", "movie_credit_reassignments", "movie_translations", "movie_tags"} {
				var count int64
				require.NoError(t, db.Table(table).Count(&count).Error)
				require.Zero(t, count, table)
			}
			var count int64
			require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
			require.EqualValues(t, 2, count, "deleting a movie must retain the shared identities")
			require.NoError(t, db.Model(&models.Genre{}).Count(&count).Error)
			require.EqualValues(t, 1, count, "deleting a movie must retain the shared genre")
		})
	}
}
