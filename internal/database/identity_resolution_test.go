package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestIdentityResolutionAliasAndDMMHierarchy(t *testing.T) {
	db := newCreditTestDB(t)
	a := models.Actress{DMMID: 321, FirstName: "Truth", LastName: "Original", JapaneseName: "本名", Verified: true}
	require.NoError(t, db.Create(&a).Error)
	for _, alias := range []models.ActressAlias{{AliasName: "Alias Name", CanonicalName: "Original Truth"}, {AliasName: "別名", CanonicalName: "本名"}, {AliasName: "Empty Name", CanonicalName: ""}} {
		require.NoError(t, db.Create(&alias).Error)
	}
	for _, scraped := range []models.Actress{{FirstName: "Name", LastName: "Alias"}, {JapaneseName: "別名"}, {DMMID: 321, JapaneseName: "Wrong"}, {FirstName: "Truth", LastName: "Original"}} {
		found, outcome, err := ResolveActressIdentityTx(db.DB, &scraped)
		require.NoError(t, err)
		require.Equal(t, ResolutionMatched, outcome)
		require.Equal(t, a.ID, found.ID)
	}
	candidate := models.Actress{DMMID: 654, FirstName: "Candidate", Verified: false}
	require.NoError(t, db.Create(&candidate).Error)
	found, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{DMMID: 654})
	require.NoError(t, err)
	require.Equal(t, ResolutionCandidateLinked, outcome)
	require.Equal(t, candidate.ID, found.ID)
	require.NoError(t, db.Model(&candidate).Update("name_key", "ambiguous").Error)
	_, outcome, err = ResolveActressIdentityTx(db.DB, &models.Actress{DMMID: 654})
	require.NoError(t, err)
	require.Equal(t, ResolutionAmbiguous, outcome)
	b := models.Actress{FirstName: "Other", JapaneseName: "本名", Verified: true}
	require.NoError(t, db.Create(&b).Error)
	found, outcome, err = ResolveActressIdentityTx(db.DB, &models.Actress{JapaneseName: "別名"})
	require.NoError(t, err)
	require.Equal(t, ResolutionAmbiguous, outcome)
	require.False(t, found.Verified)
	repeated, again, err := ResolveActressIdentityTx(db.DB, &models.Actress{JapaneseName: "別名"})
	require.NoError(t, err)
	require.Equal(t, outcome, again)
	require.Equal(t, found.ID, repeated.ID)
	emptyMatches, err := findVerifiedByAliasTx(db.DB, "", "Name", "Empty")
	require.NoError(t, err)
	require.Empty(t, emptyMatches)
	_, _, err = ResolveActressIdentityTx(db.DB, nil)
	require.Error(t, err)
	require.Empty(t, actressNameKey(nil))
	require.Empty(t, actressNameKey(&models.Actress{}))
	_, err = findVerifiedByDMMIDTx(db.DB, 0)
	require.Error(t, err)
	_, err = findCandidateByNameKeyTx(db.DB, "")
	require.Error(t, err)
	_, err = findCandidateByDMMIDTx(db.DB, 0)
	require.Error(t, err)
	_, err = resolveAmbiguousCandidateTx(db.DB, &models.Actress{}, "")
	require.Error(t, err)
}

func TestAmbiguousDMMCandidatesRemainSeparate(t *testing.T) {
	db := newCreditTestDB(t)
	for i := 0; i < 2; i++ {
		require.NoError(t, db.Create(&models.Actress{
			JapaneseName: "同名", FirstName: "Same", LastName: "Name", Verified: true, Origin: ActressOriginUser,
		}).Error)
	}

	first, firstOutcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{
		DMMID: 2001, JapaneseName: "同名", FirstName: "Same", LastName: "Name",
	})
	require.NoError(t, err)
	require.Equal(t, ResolutionAmbiguous, firstOutcome)
	require.False(t, first.Verified)
	require.Equal(t, 2001, first.DMMID)

	second, secondOutcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{
		DMMID: 2002, JapaneseName: "同名", FirstName: "Same", LastName: "Name",
	})
	require.NoError(t, err)
	require.Equal(t, ResolutionAmbiguous, secondOutcome)
	require.False(t, second.Verified)
	require.Equal(t, 2002, second.DMMID)
	require.NotEqual(t, first.ID, second.ID)

	byDMM, err := findCandidateByDMMIDTx(db.DB, 2002)
	require.NoError(t, err)
	require.Equal(t, second.ID, byDMM.ID)
	reused, err := resolveAmbiguousCandidateTx(db.DB, &models.Actress{DMMID: 2002}, "same")
	require.NoError(t, err)
	require.Equal(t, second.ID, reused.ID)

	repeated, repeatedOutcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{
		DMMID: 2002, JapaneseName: "別名", FirstName: "Other", LastName: "Name",
	})
	require.NoError(t, err)
	require.Equal(t, ResolutionCandidateLinked, repeatedOutcome)
	require.Equal(t, second.ID, repeated.ID)

	candidates, err := NewActressRepository(db).ListCandidates(context.Background(), 100, 0)
	require.NoError(t, err)
	require.Len(t, candidates, 2)
}

func TestIdentityResolutionDatabaseErrors(t *testing.T) {
	db := newCreditTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := db.WithContext(ctx)
	for _, a := range []models.Actress{{DMMID: 1}, {JapaneseName: "名前"}, {FirstName: "Only"}, {}} {
		_, _, err := ResolveActressIdentityTx(tx, &a)
		require.Error(t, err)
	}
	_, err := findVerifiedByNameTx(tx, "名前", "", "")
	require.Error(t, err)
	_, err = findCandidateByNameKeyTx(tx, "key")
	require.Error(t, err)
	_, err = resolveAmbiguousCandidateTx(tx, &models.Actress{}, "key")
	require.Error(t, err)
	_, err = resolveAmbiguousCandidateTx(tx, &models.Actress{DMMID: 42}, "key")
	require.Error(t, err)
	_, err = createCandidateTx(tx, &models.Actress{}, "")
	require.Error(t, err)
}

func TestCreditUpsertPreservesUserOwnershipAndPinnedOrder(t *testing.T) {
	db, service, credit, collision := collisionFixture(t)
	ctx := context.Background()
	require.NoError(t, service.Credits.UpdateOrderPinned(ctx, credit.ID, 9, true))
	refreshed := models.MovieCredit{MovieContentID: credit.MovieContentID, ActressID: credit.ActressID, CreditedName: "Fresh", OrderIndex: 1, Origin: "scrape"}
	require.NoError(t, service.Credits.UpsertTx(db.DB, &refreshed))
	saved, err := service.Credits.FindByCreditID(ctx, credit.ID)
	require.NoError(t, err)
	require.Equal(t, "Fresh", saved.CreditedName)
	require.Equal(t, 9, saved.OrderIndex)
	require.Equal(t, "user", saved.Origin)
	require.NoError(t, service.Credits.UpdateSuppressed(ctx, credit.ID, true))
	refreshed.CreditedName = "Do not resurrect"
	require.NoError(t, service.Credits.UpsertTx(db.DB, &refreshed))
	require.True(t, refreshed.Suppressed)
	target := models.Actress{FirstName: "Target", Verified: true}
	require.NoError(t, db.Create(&target).Error)
	dest := models.MovieCredit{MovieContentID: credit.MovieContentID, ActressID: target.ID}
	require.NoError(t, service.Credits.UpsertTx(db.DB, &dest))
	require.NoError(t, service.Collisions.TransferTx(db.DB, credit.ID, dest.ID, credit.MovieContentID))
	require.NoError(t, db.First(&collision, collision.ID).Error)
	require.Equal(t, dest.ID, collision.CreditID)
	require.NoError(t, service.Credits.DeleteByIDTx(db.DB, credit.ID))
	_, err = service.Credits.FindByCreditID(ctx, credit.ID)
	require.ErrorIs(t, err, ErrNotFound)
}
