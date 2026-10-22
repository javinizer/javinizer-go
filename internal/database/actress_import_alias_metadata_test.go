package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestImportedInlineAliasesRemainUntrustedMetadata(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	owner := models.Actress{JapaneseName: "Actual owner", Verified: true, Origin: ActressOriginUser}
	candidate := models.Actress{DMMID: 900001, JapaneseName: "Unverified stage", Origin: ActressOriginScrape}
	require.NoError(t, db.Create(&owner).Error)
	require.NoError(t, db.Create(&candidate).Error)
	curated := models.ActressAlias{AliasName: "Curated stage", CanonicalName: owner.JapaneseName}
	require.NoError(t, db.Create(&curated).Error)
	movie := &models.Movie{ID: "inline-alias-metadata", ContentID: "inline-alias-metadata"}
	require.NoError(t, db.Create(movie).Error)
	require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID, CreditedName: candidate.JapaneseName}).Error)
	var creditsBefore []models.MovieCredit
	require.NoError(t, db.Order("id").Find(&creditsBefore).Error)
	require.Len(t, creditsBefore, 1)
	// Both imported identities claim the same incorrect/ambiguous strings as
	// metadata. Neither claim is permission to steal names or existing credits.
	inline := "Actual owner|Curated stage|Unverified stage|Unknown stage"
	for _, name := range []string{"Imported one", "Imported two"} {
		incoming := models.Actress{JapaneseName: name, Aliases: inline}
		require.NoError(t, repo.ImportUpsert(t.Context(), &incoming))
		require.NotEqual(t, owner.ID, incoming.ID)
		require.NotEqual(t, candidate.ID, incoming.ID)
		var stored models.Actress
		require.NoError(t, db.First(&stored, incoming.ID).Error)
		require.Equal(t, inline, stored.Aliases)
		// Reimporting the metadata must not create trusted alias rows either.
		require.NoError(t, repo.ImportUpsert(t.Context(), &incoming))
	}
	var aliases []models.ActressAlias
	require.NoError(t, db.Order("id").Find(&aliases).Error)
	require.Equal(t, []models.ActressAlias{curated}, aliases)
	for _, name := range []string{"Actual owner", "Curated stage"} {
		found, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{JapaneseName: name})
		require.NoError(t, err)
		require.Equal(t, ResolutionMatched, outcome)
		require.Equal(t, owner.ID, found.ID)
	}
	found, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{DMMID: candidate.DMMID, JapaneseName: candidate.JapaneseName})
	require.NoError(t, err)
	require.Equal(t, ResolutionCandidateLinked, outcome)
	require.Equal(t, candidate.ID, found.ID)
	require.False(t, found.AmbiguityQuarantined, "inline aliases must not supply quarantine evidence")
	unknown, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{JapaneseName: "Unknown stage"})
	require.NoError(t, err)
	require.Equal(t, ResolutionCandidateLinked, outcome)
	require.False(t, unknown.Verified)
	// A later import named only by an inline alias must match this new candidate,
	// never either of the unrelated imported rows claiming that alias.
	incoming := models.Actress{JapaneseName: "Unknown stage"}
	require.NoError(t, repo.ImportUpsert(t.Context(), &incoming))
	require.Equal(t, unknown.ID, incoming.ID)
	var creditsAfter []models.MovieCredit
	require.NoError(t, db.Order("id").Find(&creditsAfter).Error)
	require.Equal(t, creditsBefore, creditsAfter)
}
