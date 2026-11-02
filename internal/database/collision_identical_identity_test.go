package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestIdenticalCollisionNamesPreserveDistinctIdentities(t *testing.T) {
	db := newCreditTestDB(t)
	repo := db.Repositories()
	identities := []models.Actress{
		{DMMID: 1109243, FirstName: "Nagisa", LastName: "Hazuki", JapaneseName: "葉月なぎさ", Verified: true, Origin: "user"},
		{DMMID: 1034095, FirstName: "Nagisa", LastName: "Hazuki", JapaneseName: "葉月渚", Verified: true, Origin: "user"},
	}
	for i := range identities {
		require.NoError(t, repo.ActressRepo.Create(t.Context(), &identities[i]))
	}
	movie := creditMovie("equal-display-names", []models.MovieCredit{{CreditedName: "Hazuki Nagisa", Source: "fixture", Scraped: models.Actress{FirstName: "Nagisa", LastName: "Hazuki"}}})
	_, err := repo.MovieRepo.UpsertWithTranslations(t.Context(), movie, nil, nil)
	require.NoError(t, err)
	collisions, err := repo.CreditCollisionRepo.ListOpenByMovie(t.Context(), movie.ContentID)
	require.NoError(t, err)
	require.Len(t, collisions, 1)
	require.Equal(t, models.CreditFieldIdentityLink, collisions[0].Field)
	require.Equal(t, "Hazuki Nagisa", collisions[0].ReportedValue)
	require.Equal(t, collisions[0].ReportedValue, collisions[0].CanonicalValue)
	require.Equal(t, models.CollisionStatusOpen, collisions[0].Status)
	context, err := NewCollisionService(db).ActionContexts(t.Context(), collisions)
	require.NoError(t, err)
	require.NotContains(t, []uint{identities[0].ID, identities[1].ID}, context[collisions[0].ID].CurrentActressID)
	require.ElementsMatch(t, []string{models.CollisionResolutionAdoptCanonical, models.CollisionResolutionReassign}, context[collisions[0].ID].AllowedResolutions)
	for _, identity := range identities {
		var after models.Actress
		require.NoError(t, db.First(&after, identity.ID).Error)
		require.Equal(t, identity.DMMID, after.DMMID)
		require.Equal(t, identity.JapaneseName, after.JapaneseName)
		require.True(t, after.Verified)
	}
}
