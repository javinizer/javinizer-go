package database

import (
	"errors"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Snapshot the durable rows, not the operation-owned movie: upsert is allowed to
// resolve IDs in its input even when its transaction ultimately rolls back.
func moviePersistenceRows(t *testing.T, db *gorm.DB) map[string][]map[string]any {
	t.Helper()
	rows := make(map[string][]map[string]any)
	for _, table := range []struct{ name, order string }{
		{"movies", "content_id"}, {"actresses", "id"}, {"genres", "id"},
		{"movie_credits", "id"}, {"credit_collisions", "id"}, {"actress_aliases", "id"},
		{"movie_translations", "id"}, {"actress_translations", "id"}, {"genre_translations", "id"},
		{"movie_credit_reassignments", "id"},
		{"movie_actresses", "movie_content_id, actress_id"}, {"movie_genres", "movie_content_id, genre_id"},
	} {
		var records []map[string]any
		require.NoError(t, db.Session(&gorm.Session{NewDB: true}).Table(table.name).Order(table.order).Find(&records).Error)
		rows[table.name] = records
	}
	return rows
}

func moviePersistenceFixture(t *testing.T) (*DB, *models.Movie, models.Actress) {
	t.Helper()
	db := newCreditTestDB(t)
	actress := models.Actress{DMMID: 879001, LastName: "Canonical", FirstName: "Person", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actress).Error)
	genre := models.Genre{Name: "Original genre"}
	require.NoError(t, db.Create(&genre).Error)
	var target *models.Movie
	for _, contentID := range []string{"persistence-target", "persistence-unrelated"} {
		movie := &models.Movie{
			ContentID: contentID, ID: strings.ToUpper(contentID), Title: "Original title",
			Actresses: []models.Actress{actress}, Genres: []models.Genre{genre},
			Translations: []models.MovieTranslation{{Language: "en", Title: "Original translation", SettingsHash: "original"}},
		}
		require.NoError(t, db.Create(movie).Error)
		credit := models.MovieCredit{
			MovieContentID: contentID, ActressID: actress.ID, CreditedName: actress.FullName(),
			Origin: "user", UserOverride: true, OverrideName: "Curated display", OrderPinned: true, OrderIndex: 7,
		}
		require.NoError(t, db.Create(&credit).Error)
		require.NoError(t, db.Create(&models.CreditCollision{
			CreditID: credit.ID, MovieContentID: contentID, Field: models.CreditFieldReportedThumb,
			ReportedValue: "curated-evidence.jpg", CanonicalValue: "canonical.jpg", Status: models.CollisionStatusOpen, UserPinned: true,
		}).Error)
		if target == nil {
			target = movie
		}
	}
	require.NoError(t, db.Create(&models.ActressAlias{AliasName: "Existing alias", CanonicalName: actress.FullName()}).Error)
	require.NoError(t, db.Create(&models.ActressTranslation{ActressID: actress.ID, Language: "en", FirstName: "Original actress"}).Error)
	require.NoError(t, db.Create(&models.GenreTranslation{GenreID: genre.ID, Language: "en", Name: "Original genre translation"}).Error)
	return db, target, actress
}

func moviePersistenceInput(movie *models.Movie, actress models.Actress) *models.Movie {
	return &models.Movie{
		ContentID: movie.ContentID, ID: movie.ID, Title: "Updated title",
		Genres: []models.Genre{{Name: "New genre"}}, Actresses: []models.Actress{actress},
		Translations: []models.MovieTranslation{{Language: "en", Title: "Updated translation", SettingsHash: "updated"}},
		CreditPolicy: "auto_alias", TrustedCollisionSources: []string{"trusted"},
		Credits: []models.MovieCredit{{
			ActressID: actress.ID, CreditedName: "New stage name", Source: "trusted", Origin: "scrape",
			Scraped: models.Actress{DMMID: actress.DMMID, LastName: "New", FirstName: "Stage"},
		}},
	}
}

func moviePersistenceHasWhere(tx *gorm.DB, sql string) bool {
	where, ok := tx.Statement.Clauses["WHERE"].Expression.(clause.Where)
	if !ok {
		return false
	}
	for _, expression := range where.Exprs {
		if expression, ok := expression.(clause.Expr); ok && expression.SQL == sql {
			return true
		}
	}
	return false
}

// Fail exactly one identified query boundary after SQLite has read the row but
// before GORM preloads associations. No rows or schema are sabotaged, and no
// query-count assumptions couple the test to unrelated resolver work.
func failMoviePersistenceQuery(t *testing.T, db *DB, match func(*gorm.DB) bool) (error, *int, *map[string][]map[string]any) {
	t.Helper()
	failure := errors.New("movie persistence read failed")
	fired := 0
	var inTransaction map[string][]map[string]any
	name := "test:movie-persistence-read"
	require.NoError(t, db.Callback().Query().After("gorm:query").Before("gorm:preload").Register(name, func(tx *gorm.DB) {
		if fired == 0 && tx.Error == nil && match(tx) {
			fired++
			inTransaction = moviePersistenceRows(t, tx)
			_ = tx.AddError(failure)
		}
	}))
	t.Cleanup(func() { require.NoError(t, db.Callback().Query().Remove(name)) })
	return failure, &fired, &inTransaction
}

func TestMovieUpserterPersistenceFailureRollback(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		prepare    func(*models.Movie)
		match      func(*gorm.DB) bool
		writes     bool
	}{
		{
			name: "display ID fallback", want: "find movie PERSISTENCE-TARGET",
			prepare: func(movie *models.Movie) { movie.ContentID = "not-persisted" },
			match: func(tx *gorm.DB) bool {
				return tx.Statement.Table == "movies" && moviePersistenceHasWhere(tx, "id = ?")
			},
		},
		{
			name: "legacy actress primary key", want: "ensure actresses for movie persistence-target",
			prepare: func(movie *models.Movie) { movie.Credits = nil },
			match: func(tx *gorm.DB) bool {
				_, singleActress := tx.Statement.Dest.(*models.Actress)
				return singleActress && strings.Contains(tx.Statement.SQL.String(), "WHERE `actresses`.`id` = ?")
			},
			writes: true,
		},
		{
			name: "credit projection primary key", want: "movie persistence read failed",
			match: func(tx *gorm.DB) bool {
				_, singleActress := tx.Statement.Dest.(*models.Actress)
				return singleActress && strings.Contains(tx.Statement.SQL.String(), "WHERE `actresses`.`id` = ?")
			},
			writes: true,
		},
		{
			name: "translation identity", want: "reload resolved credit actresses for translations",
			match: func(tx *gorm.DB) bool {
				return tx.Statement.Table == "actresses" && moviePersistenceHasWhere(tx, "id IN ?")
			},
			writes: true,
		},
		{
			name: "genre translation carry fields", want: "save movie persistence-target: reload genres persistence-target",
			match: func(tx *gorm.DB) bool {
				return tx.Statement.Table == "genres" && strings.Contains(tx.Statement.SQL.String(), "JOIN `movie_genres`")
			},
			writes: true,
		},
		{
			name: "actress translation carry fields", want: "save movie persistence-target: reload actresses persistence-target",
			match: func(tx *gorm.DB) bool {
				return tx.Statement.Table == "actresses" && strings.Contains(tx.Statement.SQL.String(), "JOIN `movie_actresses`")
			},
			writes: true,
		},
		{
			name: "post write movie reload", want: "reload movie persistence-target",
			match: func(tx *gorm.DB) bool {
				movie, ok := tx.Statement.Dest.(*models.Movie)
				return ok && movie.Title == "Updated title" && len(tx.Statement.Preloads) == 3
			},
			writes: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, existing, actress := moviePersistenceFixture(t)
			before := moviePersistenceRows(t, db.DB)
			input := moviePersistenceInput(existing, actress)
			if tc.prepare != nil {
				tc.prepare(input)
			}
			failure, fired, inTransaction := failMoviePersistenceQuery(t, db, tc.match)
			result, err := creditCoverageUpserter(db).UpsertWithTranslations(t.Context(), input,
				[]models.GenreTranslationData{{GenreIndex: 0, Language: "en", Name: "New translated genre"}},
				[]models.ActressTranslationData{{ActressIndex: 0, Language: "en", FirstName: "New translated actress"}})
			require.Equal(t, 1, *fired, "must exercise the intended database boundary")
			require.ErrorIs(t, err, failure)
			require.ErrorContains(t, err, tc.want)
			require.Nil(t, result)
			if tc.writes {
				require.Len(t, (*inTransaction)["genres"], len(before["genres"])+1, "a real write must precede the injected failure")
			}
			if tc.name == "translation identity" || tc.name == "post write movie reload" {
				require.Len(t, (*inTransaction)["actress_aliases"], len(before["actress_aliases"])+1, "credit reconciliation must have created the accepted alias")
				require.NotEqual(t, before["movie_credits"], (*inTransaction)["movie_credits"])
			}
			if tc.name == "post write movie reload" {
				for _, table := range []string{"movies", "movie_genres", "movie_translations", "actress_translations", "genre_translations"} {
					require.NotEqual(t, before[table], (*inTransaction)[table], table+" must be written before reload")
				}
			}
			require.Equal(t, before, moviePersistenceRows(t, db.DB), "rollback must restore every row, including curated and unrelated records")
		})
	}
}

// A repeated incoming name creates a real uniqueness failure under the normal
// SQLite configuration. Even though the first genre was already inserted, the
// failed upsert must not retain that row or alter the existing movie.
func TestMovieUpserterGenreConstraintRollback(t *testing.T) {
	db, existing, actress := moviePersistenceFixture(t)
	before := moviePersistenceRows(t, db.DB)
	input := moviePersistenceInput(existing, actress)
	input.Genres = []models.Genre{{Name: "Repeated new genre"}, {Name: "Repeated new genre"}}
	result, err := creditCoverageUpserter(db).Upsert(t.Context(), input)
	var constraint sqlite3.Error
	require.ErrorAs(t, err, &constraint)
	require.Equal(t, sqlite3.ErrConstraintUnique, constraint.ExtendedCode)
	require.ErrorContains(t, err, "ensure genres for movie persistence-target")
	require.Nil(t, result)
	require.NotZero(t, input.Genres[0].ID, "first insert must succeed before the second violates uniqueness")
	require.Equal(t, before, moviePersistenceRows(t, db.DB))
}
