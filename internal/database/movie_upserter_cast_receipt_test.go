package database

import (
	"context"
	"errors"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestUpsertNormalizesInputCastToDetachedResult(t *testing.T) {
	for _, credits := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "credits"}[credits], func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewMovieRepository(db)
			actor := models.Actress{FirstName: "First", LastName: "Actor", Verified: true, Origin: "user"}
			require.NoError(t, db.Create(&actor).Error)
			input := &models.Movie{ID: "NORMALIZED", ContentID: "normalized", Actresses: []models.Actress{actor, actor}}
			if credits {
				input.Credits = []models.MovieCredit{{ActressID: actor.ID, CreditedName: actor.FullName(), Actress: &actor}, {ActressID: actor.ID, CreditedName: actor.FullName(), Actress: &actor}}
			}
			saved, err := repo.Upsert(t.Context(), input)
			require.NoError(t, err)
			require.Len(t, input.Actresses, 1)
			require.Equal(t, saved.Actresses, input.Actresses)
			persisted, err := repo.FindByContentID(t.Context(), input.ContentID)
			require.NoError(t, err)
			require.Equal(t, models.ActressCastVersion(persisted.Actresses), models.ActressCastVersion(input.Actresses))
			saved.Actresses[0].FirstName = "Detached return"
			require.Equal(t, "First", input.Actresses[0].FirstName)
			input.Actresses[0].LastName = "Detached input"
			require.Equal(t, "Actor", saved.Actresses[0].LastName)
		})
	}
}

func TestNormalizedInputCastDoesNotEscapeOuterRollback(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewMovieRepository(db)
	saved, err := repo.Upsert(t.Context(), &models.Movie{ID: "ROLLBACK", ContentID: "rollback", Actresses: []models.Actress{{FirstName: "Original"}}})
	require.NoError(t, err)
	candidate := saved.Clone()
	candidate.Actresses = append(candidate.Actresses, candidate.Actresses[0])
	sentinel := errors.New("outer envelope failure")
	err = db.WithEditTx(t.Context(), func(u EditUnit) error {
		accepted, err := u.Movies.Upsert(t.Context(), candidate)
		require.NoError(t, err)
		require.Len(t, candidate.Actresses, 1)
		require.Equal(t, accepted.Actresses, candidate.Actresses)
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	persisted, err := repo.FindByContentID(t.Context(), saved.ContentID)
	require.NoError(t, err)
	require.Equal(t, saved.Actresses, persisted.Actresses)
}

// Rendering can enrich the transaction snapshot with transient translations;
// neither direction of mutation may leak across the returned/input snapshots.
func TestUpsertNormalizedCastDetachesNestedTranslations(t *testing.T) {
	db := newCreditTestDB(t)
	input := &models.Movie{ID: "DETACHED", ContentID: "detached", Actresses: []models.Actress{{FirstName: "Actor"}}}
	saved, err := UpsertWithDisplayTitle(t.Context(), NewMovieRepository(db), input, nil, nil, func(_ context.Context, movie *models.Movie) string {
		movie.Actresses[0].Translations = []models.ActressTranslation{{Language: "en", FirstName: "Translated"}}
		return "Rendered"
	})
	require.NoError(t, err)
	require.Equal(t, saved.Actresses, input.Actresses)
	saved.Actresses[0].Translations[0].FirstName = "Changed return"
	require.Equal(t, "Translated", input.Actresses[0].Translations[0].FirstName)
	input.Actresses[0].Translations[0].Language = "ja"
	require.Equal(t, "en", saved.Actresses[0].Translations[0].Language)
}

func TestUpsertNormalizesCastAfterTranslationIndexMapping(t *testing.T) {
	db := newCreditTestDB(t)
	actor := models.Actress{FirstName: "Existing", Verified: true, Origin: "user"}
	require.NoError(t, db.Create(&actor).Error)
	input := &models.Movie{ID: "INDEXED", ContentID: "indexed", Actresses: []models.Actress{actor, actor, {FirstName: "New"}}}
	saved, err := NewMovieRepository(db).UpsertWithTranslations(t.Context(), input, nil, []models.ActressTranslationData{
		{ActressIndex: 0, Language: "en", DisplayName: "First duplicate"},
		{ActressIndex: 1, Language: "en", DisplayName: "Last duplicate"},
		{ActressIndex: 2, Language: "en", DisplayName: "New translation"},
	})
	require.NoError(t, err)
	require.Len(t, input.Actresses, 2)
	require.Equal(t, saved.Actresses, input.Actresses)
	names := finalStoredTranslationNames(t, db)
	require.Equal(t, "Last duplicate", names[actor.ID])
	for _, a := range saved.Actresses {
		if a.ID != actor.ID {
			require.Equal(t, "New translation", names[a.ID])
		}
	}
}
