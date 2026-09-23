package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestCandidateMergePreservesAmbiguityUntilVerifiedIdentitySurvives(t *testing.T) {
	for _, tc := range []struct {
		name             string
		quarantineTarget bool
	}{
		{name: "quarantined target", quarantineTarget: true},
		{name: "quarantined source"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := newCreditTestDB(t)
			actresses := NewActressRepository(db)
			movies := NewMovieRepository(db)

			canonical := models.Actress{FirstName: "Same", LastName: "Person", Verified: true, Origin: ActressOriginUser}
			require.NoError(t, db.Create(&canonical).Error)
			movie, err := movies.Upsert(ctx, creditMovie("merge-ambiguity-"+tc.name, []models.MovieCredit{{
				CreditedName: "Same Person",
				Scraped:      models.Actress{DMMID: 88101, FirstName: "Same", LastName: "Person"},
			}}))
			require.NoError(t, err)
			require.Len(t, movie.Credits, 1)
			quarantinedID := movie.Credits[0].ActressID
			ordinary := models.Actress{FirstName: "Other", LastName: "Candidate", Origin: ActressOriginScrape}
			require.NoError(t, db.Create(&ordinary).Error)

			targetID, sourceID := ordinary.ID, quarantinedID
			if tc.quarantineTarget {
				targetID, sourceID = quarantinedID, ordinary.ID
			}
			beforeGeneration := movie.RenderGeneration
			result, err := actresses.Merge(ctx, targetID, sourceID, nil)
			require.NoError(t, err)
			require.False(t, result.MergedActress.Verified)

			var survivor models.Actress
			require.NoError(t, db.First(&survivor, targetID).Error)
			require.True(t, survivor.AmbiguityQuarantined)
			resolved, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{DMMID: 88101, FirstName: "Same", LastName: "Person"})
			require.NoError(t, err)
			require.Equal(t, ResolutionAmbiguous, outcome)
			require.Equal(t, targetID, resolved.ID)

			persisted, err := movies.FindByContentID(ctx, movie.ContentID)
			require.NoError(t, err)
			require.Empty(t, persisted.Actresses)
			if tc.quarantineTarget {
				require.Equal(t, beforeGeneration, persisted.RenderGeneration)
			} else {
				require.Equal(t, beforeGeneration+1, persisted.RenderGeneration)
			}
			counts, err := NewCreditCollisionRepository(db).CountOpenByMovieBatch(ctx, []string{movie.ContentID})
			require.NoError(t, err)
			require.EqualValues(t, 1, counts[movie.ContentID])
			require.ErrorIs(t, movies.WithApplyArtifactPublicationFence(ctx, movie.ContentID, persisted.RenderGeneration, func(*models.Movie) error { return nil }), ErrApplyArtifactPublicationBlocked)
		})
	}
}

func TestVerifiedSourceMergePromotesSurvivingTargetIdentity(t *testing.T) {
	ctx := context.Background()
	db := newCreditTestDB(t)
	actresses := NewActressRepository(db)
	movies := NewMovieRepository(db)

	verified := models.Actress{FirstName: "Same", LastName: "Person", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&verified).Error)
	movie, err := movies.Upsert(ctx, creditMovie("merge-verified-source", []models.MovieCredit{{
		CreditedName: "Same Person",
		Scraped:      models.Actress{DMMID: 88102, FirstName: "Same", LastName: "Person"},
	}}))
	require.NoError(t, err)
	candidateID := movie.Credits[0].ActressID

	result, err := actresses.Merge(ctx, candidateID, verified.ID, nil)
	require.NoError(t, err)
	require.True(t, result.MergedActress.Verified)
	require.Equal(t, ActressOriginUser, result.MergedActress.Origin)
	var survivor models.Actress
	require.NoError(t, db.First(&survivor, candidateID).Error)
	require.False(t, survivor.AmbiguityQuarantined)

	persisted, err := movies.FindByContentID(ctx, movie.ContentID)
	require.NoError(t, err)
	require.Len(t, persisted.Actresses, 1)
	require.Equal(t, candidateID, persisted.Actresses[0].ID)
	counts, err := NewCreditCollisionRepository(db).CountOpenByMovieBatch(ctx, []string{movie.ContentID})
	require.NoError(t, err)
	require.Zero(t, counts[movie.ContentID])
	require.NoError(t, movies.WithApplyArtifactPublicationFence(ctx, movie.ContentID, persisted.RenderGeneration, func(*models.Movie) error { return nil }))
}

func TestVerifiedMergeFailureRollsBackIdentityGateAndRenderState(t *testing.T) {
	ctx := context.Background()
	db := newCreditTestDB(t)
	actresses := NewActressRepository(db)
	movies := NewMovieRepository(db)

	verified := models.Actress{FirstName: "Same", LastName: "Person", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&verified).Error)
	movie, err := movies.Upsert(ctx, creditMovie("merge-verified-rollback", []models.MovieCredit{{
		CreditedName: "Same Person",
		Scraped:      models.Actress{DMMID: 88103, FirstName: "Same", LastName: "Person"},
	}}))
	require.NoError(t, err)
	candidateID := movie.Credits[0].ActressID
	plan, err := actresses.merger.PlanMerge(ctx, candidateID, verified.ID, nil)
	require.NoError(t, err)
	injectDatabaseCallbackError(t, db, "update", "movies", 1)

	_, err = actresses.merger.ExecuteMerge(ctx, plan, db)
	require.Error(t, err)

	var candidateAfter, verifiedAfter models.Actress
	require.NoError(t, db.First(&candidateAfter, candidateID).Error)
	require.NoError(t, db.First(&verifiedAfter, verified.ID).Error)
	require.False(t, candidateAfter.Verified)
	require.True(t, candidateAfter.AmbiguityQuarantined)
	require.True(t, verifiedAfter.Verified)
	persisted, err := movies.FindByContentID(ctx, movie.ContentID)
	require.NoError(t, err)
	require.Equal(t, movie.RenderGeneration, persisted.RenderGeneration)
	require.Empty(t, persisted.Actresses)
	require.Equal(t, candidateID, persisted.Credits[0].ActressID)
	counts, err := NewCreditCollisionRepository(db).CountOpenByMovieBatch(ctx, []string{movie.ContentID})
	require.NoError(t, err)
	require.EqualValues(t, 1, counts[movie.ContentID])
}

func TestMergeAssociationAndProjectionFailuresRemainAtomic(t *testing.T) {
	t.Run("association load", func(t *testing.T) {
		ctx := context.Background()
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		target := models.Actress{FirstName: "Target", Verified: true, Origin: ActressOriginUser}
		source := models.Actress{FirstName: "Source", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&target).Error)
		require.NoError(t, db.Create(&source).Error)
		movie := models.Movie{ContentID: "merge-association-failure", ID: "merge-association-failure", Title: "Merge failure", Actresses: []models.Actress{source}}
		require.NoError(t, db.Create(&movie).Error)
		plan, err := repo.merger.PlanMerge(ctx, target.ID, source.ID, nil)
		require.NoError(t, err)
		injectDatabaseCallbackError(t, db, "query", "movies", 2)

		_, err = repo.merger.ExecuteMerge(ctx, plan, db)
		require.Error(t, err)
		var sourceAfter models.Actress
		require.NoError(t, db.First(&sourceAfter, source.ID).Error)
	})

	t.Run("verified projection restore", func(t *testing.T) {
		ctx := context.Background()
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		movies := NewMovieRepository(db)
		verified := models.Actress{FirstName: "Same", LastName: "Person", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&verified).Error)
		movie, err := movies.Upsert(ctx, creditMovie("merge-projection-failure", []models.MovieCredit{{
			CreditedName: "Same Person",
			Scraped:      models.Actress{DMMID: 88104, FirstName: "Same", LastName: "Person"},
		}}))
		require.NoError(t, err)
		candidateID := movie.Credits[0].ActressID
		plan, err := repo.merger.PlanMerge(ctx, candidateID, verified.ID, nil)
		require.NoError(t, err)
		require.NoError(t, db.Exec(`CREATE TRIGGER fail_merge_projection BEFORE INSERT ON movie_actresses BEGIN SELECT RAISE(FAIL, 'projection failure'); END`).Error)

		_, err = repo.merger.ExecuteMerge(ctx, plan, db)
		require.ErrorContains(t, err, "projection failure")
		var candidateAfter, verifiedAfter models.Actress
		require.NoError(t, db.First(&candidateAfter, candidateID).Error)
		require.NoError(t, db.First(&verifiedAfter, verified.ID).Error)
		require.False(t, candidateAfter.Verified)
		require.True(t, candidateAfter.AmbiguityQuarantined)
		require.True(t, verifiedAfter.Verified)
	})
}

func TestVerifiedMergeFieldReconcileFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{FirstName: "Target", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{FirstName: "Source", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	movie := models.Movie{ContentID: "merge-reconcile-rollback", ID: "merge-reconcile-rollback", Title: "rollback", RenderGeneration: 9}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: source.ID, CreditedName: "Reported", Origin: string(models.CreditOriginScrape)}
	require.NoError(t, db.Create(&credit).Error)
	collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldCreditedName, ReportedValue: "Reported", Status: models.CollisionStatusOpen}
	require.NoError(t, db.Create(&collision).Error)
	plan, err := repo.merger.PlanMerge(ctx, target.ID, source.ID, nil)
	require.NoError(t, err)
	// The first collision update is verified identity closure; fail the following
	// field-local reconciliation to prove the later transition remains atomic.
	injectDatabaseCallbackError(t, db, "update", "credit_collisions", 2)

	_, err = repo.merger.ExecuteMerge(ctx, plan, db)
	require.Error(t, err)
	var sourceAfter models.Actress
	require.NoError(t, db.First(&sourceAfter, source.ID).Error)
	var creditAfter models.MovieCredit
	require.NoError(t, db.First(&creditAfter, credit.ID).Error)
	require.Equal(t, source.ID, creditAfter.ActressID)
	var collisionAfter models.CreditCollision
	require.NoError(t, db.First(&collisionAfter, collision.ID).Error)
	require.Equal(t, models.CollisionStatusOpen, collisionAfter.Status)
	var movieAfter models.Movie
	require.NoError(t, db.First(&movieAfter, "content_id = ?", movie.ContentID).Error)
	require.Equal(t, movie.RenderGeneration, movieAfter.RenderGeneration)
}
