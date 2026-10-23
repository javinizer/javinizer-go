package database

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPrepareMovieResolvesCallerIDsFromStoredAssociations(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)
	movie := createTestMovie("RESOLVE-001")
	movie.Genres = []models.Genre{{Name: "resolution genre"}}
	movie.Actresses = []models.Actress{
		{DMMID: 88001, JapaneseName: "識別子女優"},
		{FirstName: "Name", LastName: "Match", JapaneseName: "名前女優"},
	}
	_, err := repo.Upsert(context.Background(), movie)
	require.NoError(t, err)
	stored, err := repo.FindByID(context.Background(), movie.ContentID)
	require.NoError(t, err)
	require.Len(t, stored.Genres, 1)
	require.Len(t, stored.Actresses, 2)
	expectedIDs := make(map[string]uint)
	for _, actress := range stored.Actresses {
		expectedIDs[actress.JapaneseName] = actress.ID
	}
	require.NotZero(t, expectedIDs["識別子女優"])
	require.NotZero(t, expectedIDs["名前女優"])
	input := *stored
	input.Genres = []models.Genre{{Name: stored.Genres[0].Name}}
	input.Actresses = []models.Actress{
		{DMMID: 88001, JapaneseName: "caller spelling"},
		{FirstName: "Name", LastName: "Match", JapaneseName: "名前女優"},
	}
	prepared, err := prepareMovieForUpsert(db.DB, &input,
		[]models.GenreTranslationData{{GenreIndex: 0, Language: "en", Name: "translated"}},
		[]models.ActressTranslationData{{ActressIndex: 0, Language: "en"}, {ActressIndex: 1, Language: "en"}})
	require.NoError(t, err)
	require.Same(t, &input, prepared.movie)
	require.Equal(t, stored.Genres[0].ID, input.Genres[0].ID)
	require.Equal(t, expectedIDs["識別子女優"], input.Actresses[0].ID)
	require.Equal(t, expectedIDs["名前女優"], input.Actresses[1].ID)
	after, err := repo.FindByID(context.Background(), movie.ContentID)
	require.NoError(t, err)
	require.Equal(t, stored, after, "resolution must not modify stored associations")
}

func TestPrepareMovieCanceledAssociationQueriesPreserveInput(t *testing.T) {
	for _, association := range []string{"genres", "actresses"} {
		t.Run(association, func(t *testing.T) {
			db := newDatabaseTestDB(t)
			movie := createTestMovie("CANCEL-" + association)
			if association == "genres" {
				movie.Genres = []models.Genre{{Name: "unresolved"}}
			} else {
				movie.Actresses = []models.Actress{{JapaneseName: "未解決"}}
			}
			before, err := json.Marshal(movie)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			prepared, err := prepareMovieForUpsert(db.WithContext(ctx), movie,
				[]models.GenreTranslationData{{GenreIndex: 0, Language: "en"}}, nil)
			require.Nil(t, prepared)
			require.ErrorIs(t, err, context.Canceled)
			require.EqualError(t, err, "reload "+association+" "+movie.ContentID+": context canceled")
			after, marshalErr := json.Marshal(movie)
			require.NoError(t, marshalErr)
			require.Equal(t, before, after, "canceled resolution must not mutate caller input")
			var count int64
			require.NoError(t, db.Model(&models.Movie{}).Count(&count).Error)
			require.Zero(t, count, "a canceled read must not persist caller input")
		})
	}
}

func TestMovieTranslationRejectionRollsBackCoreUpdate(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)
	movie := createTestMovie("TRANSLATION-POLICY-001")
	_, err := repo.Upsert(context.Background(), movie)
	require.NoError(t, err)
	before, err := repo.FindByID(context.Background(), movie.ContentID)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_helper_translation BEFORE INSERT ON movie_translations BEGIN SELECT RAISE(ABORT, 'fixture movie translation rejected'); END`).Error)
	changed := *before
	changed.Title = "must roll back"
	err = db.Transaction(func(tx *gorm.DB) error {
		return upsertMovieCore(tx, db, &changed, []models.MovieTranslation{{Language: "en", Title: "blocked"}}, nil, nil)
	})
	var sqliteErr sqlite3.Error
	require.ErrorAs(t, err, &sqliteErr)
	require.Equal(t, sqlite3.ErrConstraintTrigger, sqliteErr.ExtendedCode)
	require.EqualError(t, err, "create translation "+movie.ContentID+"/en: fixture movie translation rejected")
	after, err := repo.FindByID(context.Background(), movie.ContentID)
	require.NoError(t, err)
	require.Equal(t, before, after)
	var count int64
	require.NoError(t, db.Model(&models.MovieTranslation{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestUnresolvedTranslationIdentitiesDoNotCreateOrphans(t *testing.T) {
	db := newDatabaseTestDB(t)
	movie := createTestMovie("UNRESOLVED-001")
	// These caller identities are not in the stored association set. Preparation
	// leaves their IDs unresolved; persistence must skip rather than invent IDs.
	movie.Genres = []models.Genre{{Name: "unknown genre"}}
	movie.Actresses = []models.Actress{{JapaneseName: "未登録女優"}}
	prepared, err := prepareMovieForUpsert(db.DB, movie,
		[]models.GenreTranslationData{{GenreIndex: 0, Language: "en", Name: "unknown"}},
		[]models.ActressTranslationData{{ActressIndex: 0, Language: "en", DisplayName: "unknown"}})
	require.NoError(t, err)
	require.Zero(t, movie.Genres[0].ID)
	require.Zero(t, movie.Actresses[0].ID)
	require.NoError(t, persistTranslations(db.DB, db, prepared, nil))
	for _, model := range []any{&models.GenreTranslation{}, &models.ActressTranslation{}, &models.MovieTranslation{}, &models.Movie{}} {
		var count int64
		require.NoError(t, db.Model(model).Count(&count).Error)
		require.Zero(t, count, "unresolved translations must not create records")
	}
}
