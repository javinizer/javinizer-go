package database

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestMovieRepositoryFindByIDLoadsCredits(t *testing.T) {
	db, _, credit, _ := collisionFixture(t)
	repo := NewMovieRepository(db)
	movie, err := repo.FindByID(t.Context(), credit.MovieContentID)
	require.NoError(t, err)
	require.Len(t, movie.Credits, 1)
	require.Equal(t, credit.ID, movie.Credits[0].ID)
	require.NotNil(t, movie.Credits[0].Actress)
	injectDatabaseCallbackError(t, db, "query", "movie_credits", 1)
	_, err = repo.FindByID(t.Context(), credit.MovieContentID)
	require.Error(t, err)
}

func TestDeleteCreditRecordsTxErrors(t *testing.T) {
	t.Run("collision", func(t *testing.T) {
		db := newCreditTestDB(t)
		injectDatabaseCallbackError(t, db, "delete", "credit_collisions", 1)
		err := deleteCreditRecordsTx(db.DB, "movie_content_id = ?", "movie_content_id = ?", "movie", "movie movie")
		require.ErrorContains(t, err, "credit collisions")
	})
	t.Run("credit", func(t *testing.T) {
		db := newCreditTestDB(t)
		injectDatabaseCallbackError(t, db, "delete", "movie_credits", 1)
		err := deleteCreditRecordsTx(db.DB, "movie_content_id = ?", "movie_content_id = ?", "movie", "movie movie")
		require.ErrorContains(t, err, "movie credits")
	})
}

func TestMovieDeleteCreditCleanupErrorRollsBack(t *testing.T) {
	db, _, credit, _ := collisionFixture(t)
	injectDatabaseCallbackError(t, db, "delete", "credit_collisions", 1)
	err := NewMovieRepository(db).Delete(t.Context(), credit.MovieContentID)
	require.Error(t, err)
	var movie models.Movie
	require.NoError(t, db.First(&movie, "content_id = ?", credit.MovieContentID).Error)
}

func TestMovieDeleteRemovesCreditsAndCollisions(t *testing.T) {
	db, _, credit, _ := collisionFixture(t)
	require.NoError(t, NewMovieRepository(db).Delete(t.Context(), credit.MovieContentID))
	var credits, collisions int64
	require.NoError(t, db.Model(&models.MovieCredit{}).Count(&credits).Error)
	require.NoError(t, db.Model(&models.CreditCollision{}).Count(&collisions).Error)
	require.Zero(t, credits)
	require.Zero(t, collisions)
}

func TestActressDeleteRemovesCreditsAndCollisions(t *testing.T) {
	db, _, credit, _ := collisionFixture(t)
	require.NoError(t, NewActressRepository(db).Delete(t.Context(), credit.ActressID))
	var credits, collisions int64
	require.NoError(t, db.Model(&models.MovieCredit{}).Count(&credits).Error)
	require.NoError(t, db.Model(&models.CreditCollision{}).Count(&collisions).Error)
	require.Zero(t, credits)
	require.Zero(t, collisions)
}

func TestActressDeleteErrorsRollback(t *testing.T) {
	t.Run("cleanup", func(t *testing.T) {
		db, _, credit, _ := collisionFixture(t)
		injectDatabaseCallbackError(t, db, "delete", "movie_credits", 1)
		err := NewActressRepository(db).Delete(t.Context(), credit.ActressID)
		require.Error(t, err)
		var actress models.Actress
		require.NoError(t, db.First(&actress, credit.ActressID).Error)
	})
	t.Run("actress", func(t *testing.T) {
		db := newCreditTestDB(t)
		actress := models.Actress{FirstName: "Delete"}
		require.NoError(t, db.Create(&actress).Error)
		injectDatabaseCallbackError(t, db, "delete", "actresses", 1)
		err := NewActressRepository(db).Delete(t.Context(), actress.ID)
		require.Error(t, err)
	})
}

func TestResolveActressIdentityHonorsDMMIDDuringNameFallback(t *testing.T) {
	t.Run("conflicting dmm identity is not linked", func(t *testing.T) {
		db := newCreditTestDB(t)
		existing := models.Actress{DMMID: 1001, FirstName: "Same", LastName: "Name", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&existing).Error)

		found, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{DMMID: 1002, FirstName: "Same", LastName: "Name"})
		require.NoError(t, err)
		require.Equal(t, ResolutionCandidateLinked, outcome)
		require.NotNil(t, found)
		require.NotEqual(t, existing.ID, found.ID)
		require.Equal(t, 1002, found.DMMID)
		require.False(t, found.Verified)
	})

	t.Run("dmm-less identity remains eligible", func(t *testing.T) {
		db := newCreditTestDB(t)
		withDMM := models.Actress{DMMID: 1003, FirstName: "Same", LastName: "Name", Verified: true, Origin: ActressOriginUser}
		withoutDMM := models.Actress{DMMID: 0, FirstName: "Same", LastName: "Name", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&withDMM).Error)
		require.NoError(t, db.Create(&withoutDMM).Error)

		found, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{DMMID: 1004, FirstName: "Same", LastName: "Name"})
		require.NoError(t, err)
		require.Equal(t, ResolutionMatched, outcome)
		require.NotNil(t, found)
		require.Equal(t, withoutDMM.ID, found.ID)
	})

	t.Run("conflicting dmm identity is not linked by alias", func(t *testing.T) {
		db := newCreditTestDB(t)
		existing := models.Actress{DMMID: 1005, JapaneseName: "Canonical", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&existing).Error)
		require.NoError(t, db.Create(&models.ActressAlias{AliasName: "Known Alias", CanonicalName: "Canonical"}).Error)

		found, outcome, err := ResolveActressIdentityTx(db.DB, &models.Actress{DMMID: 1006, JapaneseName: "Known Alias"})
		require.NoError(t, err)
		require.Equal(t, ResolutionCandidateLinked, outcome)
		require.NotNil(t, found)
		require.NotEqual(t, existing.ID, found.ID)
		require.Equal(t, 1006, found.DMMID)
	})
}

func TestImportUpsertRejectsAmbiguousVerifiedExactName(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	for i := 0; i < 2; i++ {
		actress := models.Actress{FirstName: "Same", LastName: "Name", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&actress).Error)
	}

	err := repo.ImportUpsert(context.Background(), &models.Actress{FirstName: "Same", LastName: "Name"})
	require.ErrorContains(t, err, "ambiguous import match")
	var count int64
	require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
}

func TestActressMergeRemovesSuppressedLegacyAssociation(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{FirstName: "Target", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{FirstName: "Source", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	movie := models.Movie{ContentID: "merge-suppressed", ID: "merge-suppressed", Title: "Merge Suppressed"}
	require.NoError(t, db.Create(&movie).Error)
	targetCredit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: target.ID, CreditedName: "Target", Origin: string(models.CreditOriginScrape)}
	sourceCredit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: source.ID, CreditedName: "Source", Origin: string(models.CreditOriginScrape), Suppressed: true}
	require.NoError(t, db.Create(&targetCredit).Error)
	require.NoError(t, db.Create(&sourceCredit).Error)
	require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?), (?, ?)", movie.ContentID, target.ID, movie.ContentID, source.ID).Error)

	_, err := repo.Merge(context.Background(), target.ID, source.ID, nil)
	require.NoError(t, err)
	var mergedCredit models.MovieCredit
	require.NoError(t, db.First(&mergedCredit, "movie_content_id = ? AND actress_id = ?", movie.ContentID, target.ID).Error)
	require.True(t, mergedCredit.Suppressed)
	var actressIDs []uint
	require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", movie.ContentID).Pluck("actress_id", &actressIDs).Error)
	require.Empty(t, actressIDs)
}

func TestActressMergeReconcilesCollisionsWithNewCanonicalName(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{FirstName: "Old", JapaneseName: "旧名", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{FirstName: "New", JapaneseName: "新名", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	targetMovie := models.Movie{ContentID: "merge-collision-target", ID: "merge-collision-target", Title: "Target Collision"}
	sourceMovie := models.Movie{ContentID: "merge-collision-source", ID: "merge-collision-source", Title: "Source Collision"}
	require.NoError(t, db.Create(&targetMovie).Error)
	require.NoError(t, db.Create(&sourceMovie).Error)
	targetCredit := models.MovieCredit{MovieContentID: targetMovie.ContentID, ActressID: target.ID, CreditedName: "旧名", Origin: string(models.CreditOriginScrape)}
	sourceCredit := models.MovieCredit{MovieContentID: sourceMovie.ContentID, ActressID: source.ID, CreditedName: "新名", Origin: string(models.CreditOriginScrape)}
	require.NoError(t, db.Create(&targetCredit).Error)
	require.NoError(t, db.Create(&sourceCredit).Error)
	for _, credit := range []models.MovieCredit{targetCredit, sourceCredit} {
		collision := models.CreditCollision{
			CreditID: credit.ID, MovieContentID: credit.MovieContentID, Field: models.CreditFieldCreditedName,
			ReportedValue: "新名", CanonicalValue: "旧名", Status: models.CollisionStatusOpen, Occurrences: 1,
		}
		require.NoError(t, db.Create(&collision).Error)
	}

	_, err := repo.Merge(context.Background(), target.ID, source.ID, map[string]string{"japanese_name": "source"})
	require.NoError(t, err)
	var collisions []models.CreditCollision
	require.NoError(t, db.Order("id").Find(&collisions).Error)
	require.Len(t, collisions, 2)
	for _, collision := range collisions {
		require.Equal(t, "新名", collision.CanonicalValue)
		require.Equal(t, models.CollisionStatusResolved, collision.Status)
		require.Equal(t, models.CollisionResolutionAdoptCanonical, collision.Resolution)
	}
}

func TestActressMergeRetargetsAliasesWithNewCanonicalName(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{FirstName: "Old", JapaneseName: "旧名", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{FirstName: "New", JapaneseName: "新名", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	alias := models.ActressAlias{AliasName: "Legacy Alias", CanonicalName: "旧名"}
	require.NoError(t, db.Create(&alias).Error)

	_, err := repo.Merge(context.Background(), target.ID, source.ID, map[string]string{"japanese_name": "source"})
	require.NoError(t, err)
	require.NoError(t, db.First(&alias, alias.ID).Error)
	require.Equal(t, "新名", alias.CanonicalName)
}

func TestActressMergeReturnsCollisionReconciliationError(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{FirstName: "Target", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{FirstName: "Source", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	plan, err := repo.merger.PlanMerge(context.Background(), target.ID, source.ID, nil)
	require.NoError(t, err)
	require.NoError(t, db.DB.Exec("DROP TABLE credit_collisions").Error)

	_, err = repo.merger.ExecuteMerge(context.Background(), plan, db)
	require.Error(t, err)
}

func TestMoveCreditsReturnsSuppressedAssociationDeleteError(t *testing.T) {
	db := newCreditTestDB(t)
	target := models.Actress{FirstName: "Target", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{FirstName: "Source", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	targetCredit := models.MovieCredit{MovieContentID: "suppressed-delete-error", ActressID: target.ID, CreditedName: "Target", Origin: string(models.CreditOriginScrape)}
	sourceCredit := models.MovieCredit{MovieContentID: "suppressed-delete-error", ActressID: source.ID, CreditedName: "Source", Origin: string(models.CreditOriginScrape), Suppressed: true}
	require.NoError(t, db.Create(&targetCredit).Error)
	require.NoError(t, db.Create(&sourceCredit).Error)
	require.NoError(t, db.DB.Exec("DROP TABLE movie_actresses").Error)

	require.Error(t, moveCredits(db.DB, source.ID, target.ID))
}

func TestActressMergeRemovesSuppressedLegacyAssociationWithoutDuplicateCredit(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{FirstName: "Target", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{FirstName: "Source", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	movie := models.Movie{ContentID: "merge-suppressed-only", ID: "merge-suppressed-only", Title: "Merge Suppressed Only"}
	require.NoError(t, db.Create(&movie).Error)
	sourceCredit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: source.ID, CreditedName: "Source", Origin: string(models.CreditOriginScrape), Suppressed: true}
	require.NoError(t, db.Create(&sourceCredit).Error)
	require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", movie.ContentID, source.ID).Error)

	_, err := repo.Merge(context.Background(), target.ID, source.ID, nil)
	require.NoError(t, err)
	var mergedCredit models.MovieCredit
	require.NoError(t, db.First(&mergedCredit, "movie_content_id = ? AND actress_id = ?", movie.ContentID, target.ID).Error)
	require.True(t, mergedCredit.Suppressed)
	var actressIDs []uint
	require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", movie.ContentID).Pluck("actress_id", &actressIDs).Error)
	require.Empty(t, actressIDs)
}

func TestActressMergeReturnsAliasRetargetError(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{FirstName: "Old", JapaneseName: "旧名", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{FirstName: "New", JapaneseName: "新名", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	require.NoError(t, db.Create(&models.ActressAlias{AliasName: "Legacy Alias", CanonicalName: "旧名"}).Error)
	plan, err := repo.merger.PlanMerge(context.Background(), target.ID, source.ID, map[string]string{"japanese_name": "source"})
	require.NoError(t, err)
	require.NoError(t, db.DB.Exec("DROP TABLE actress_aliases").Error)

	_, err = repo.merger.ExecuteMerge(context.Background(), plan, db)
	require.Error(t, err)
}

func TestMoveCreditsReturnsSuppressedAssociationDeleteErrorWithoutDuplicateCredit(t *testing.T) {
	db := newCreditTestDB(t)
	target := models.Actress{FirstName: "Target", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{FirstName: "Source", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	sourceCredit := models.MovieCredit{MovieContentID: "suppressed-delete-error-only", ActressID: source.ID, CreditedName: "Source", Origin: string(models.CreditOriginScrape), Suppressed: true}
	require.NoError(t, db.Create(&sourceCredit).Error)
	require.NoError(t, db.DB.Exec("DROP TABLE movie_actresses").Error)

	require.Error(t, moveCredits(db.DB, source.ID, target.ID))
}

func TestActressMergeReturnsSourceLookupError(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{FirstName: "Target", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{FirstName: "Source", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	plan, err := repo.merger.PlanMerge(context.Background(), target.ID, source.ID, nil)
	require.NoError(t, err)
	require.NoError(t, db.DB.Exec("DROP TABLE actresses").Error)

	_, err = repo.merger.ExecuteMerge(context.Background(), plan, db)
	require.Error(t, err)
}

func TestActressMergeReturnsTargetLookupError(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{DMMID: 11001, FirstName: "Target", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{DMMID: 11002, FirstName: "Source", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	plan, err := repo.merger.PlanMerge(context.Background(), target.ID, source.ID, map[string]string{"dmm_id": "source"})
	require.NoError(t, err)
	plan.TargetID = 999999

	_, err = repo.merger.ExecuteMerge(context.Background(), plan, db)
	require.Error(t, err)
}

func TestActressMergeMapsDuplicateKeyUpdateError(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{FirstName: "Target", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{FirstName: "Source", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	plan, err := repo.merger.PlanMerge(context.Background(), target.ID, source.ID, nil)
	require.NoError(t, err)
	callbackName := "test:merge_duplicate_actress_update"
	require.NoError(t, db.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "actresses" {
			_ = tx.AddError(gorm.ErrDuplicatedKey)
		}
	}))
	t.Cleanup(func() { _ = db.DB.Callback().Update().Remove(callbackName) })

	_, err = repo.merger.ExecuteMerge(context.Background(), plan, db)
	require.ErrorIs(t, err, ErrActressMergeUniqueConstraint)
}

func TestActressMergeReturnsGenericTargetUpdateError(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{FirstName: "Target", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{FirstName: "Source", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	plan, err := repo.merger.PlanMerge(context.Background(), target.ID, source.ID, nil)
	require.NoError(t, err)
	injectDatabaseCallbackError(t, db, "update", "actresses", 1)

	_, err = repo.merger.ExecuteMerge(context.Background(), plan, db)
	require.Error(t, err)
}
