package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func seedLiveCollisionFenceFixture(t *testing.T, db *DB, contentID string, field string) (models.MovieCredit, models.CreditCollision) {
	t.Helper()
	seedArtifactPublicationMovie(t, db, contentID, 1, true)
	actress := models.Actress{FirstName: "Fence", LastName: "Actress", Verified: true, Origin: "user"}
	require.NoError(t, db.Create(&actress).Error)
	credit := models.MovieCredit{MovieContentID: contentID, ActressID: actress.ID, CreditedName: "Reported Fence", Origin: string(models.CreditOriginScrape)}
	require.NoError(t, db.Create(&credit).Error)
	collision := models.CreditCollision{
		CreditID:       credit.ID,
		MovieContentID: contentID,
		Field:          field,
		ReportedValue:  "Reported Fence",
		CanonicalValue: "Actress Fence",
		Status:         models.CollisionStatusOpen,
		Occurrences:    1,
	}
	require.NoError(t, db.Create(&collision).Error)
	return credit, collision
}

func assertArtifactFenceResult(t *testing.T, db *DB, contentID string, wantBlocked bool) {
	t.Helper()
	var movie models.Movie
	require.NoError(t, db.Select("render_generation").First(&movie, "content_id = ?", contentID).Error)
	called := false
	err := NewMovieRepository(db).WithApplyArtifactPublicationFence(t.Context(), contentID, movie.RenderGeneration, func(*models.Movie) error {
		called = true
		return nil
	})
	if wantBlocked {
		require.ErrorIs(t, err, ErrApplyArtifactPublicationBlocked)
		require.False(t, called)
		return
	}
	require.NoError(t, err)
	require.True(t, called)
}

func TestPR260ArtifactPublicationFenceUsesLiveCollisionAttachment(t *testing.T) {
	t.Run("detached open collision is inert", func(t *testing.T) {
		db := setupBaseRepoTestDB(t)
		seedArtifactPublicationMovie(t, db, "detached", 1, true)
		require.NoError(t, db.Create(&models.CreditCollision{CreditID: 999999, MovieContentID: "detached", Field: models.CreditFieldIdentityLink, Status: models.CollisionStatusOpen}).Error)
		assertArtifactFenceResult(t, db, "detached", false)
	})

	for _, field := range []string{models.CreditFieldIdentityLink, models.CreditFieldCreditedName, models.CreditFieldReportedThumb} {
		t.Run("active attached "+field+" blocks", func(t *testing.T) {
			db := setupBaseRepoTestDB(t)
			seedLiveCollisionFenceFixture(t, db, "active-"+field, field)
			assertArtifactFenceResult(t, db, "active-"+field, true)
		})
	}

	t.Run("repository suppression is reversible without closing evidence", func(t *testing.T) {
		db := setupBaseRepoTestDB(t)
		credit, collision := seedLiveCollisionFenceFixture(t, db, "repository-suppression", models.CreditFieldCreditedName)
		repo := NewMovieCreditRepository(db)
		require.NoError(t, repo.UpdateSuppressed(t.Context(), credit.ID, true))
		assertArtifactFenceResult(t, db, credit.MovieContentID, false)
		require.NoError(t, repo.UpdateSuppressed(t.Context(), credit.ID, false))
		var stored models.CreditCollision
		require.NoError(t, db.First(&stored, collision.ID).Error)
		require.Equal(t, models.CollisionStatusOpen, stored.Status)
		assertArtifactFenceResult(t, db, credit.MovieContentID, true)
	})

	t.Run("moved credit gates only its current movie", func(t *testing.T) {
		db := setupBaseRepoTestDB(t)
		credit, _ := seedLiveCollisionFenceFixture(t, db, "move-old", models.CreditFieldCreditedName)
		seedArtifactPublicationMovie(t, db, "move-current", 1, true)
		require.NoError(t, db.Model(&models.MovieCredit{}).Where("id = ?", credit.ID).Update("movie_content_id", "move-current").Error)
		assertArtifactFenceResult(t, db, "move-old", false)
		assertArtifactFenceResult(t, db, "move-current", true)
	})

	t.Run("resolved suppression evidence is inert", func(t *testing.T) {
		db := setupBaseRepoTestDB(t)
		_, collision := seedLiveCollisionFenceFixture(t, db, "resolved", models.CreditFieldCreditedName)
		require.NoError(t, db.Model(&models.CreditCollision{}).Where("id = ?", collision.ID).Updates(map[string]interface{}{
			"status": models.CollisionStatusResolved, "resolution": models.CollisionResolutionBySuppression,
		}).Error)
		assertArtifactFenceResult(t, db, "resolved", false)
	})

	t.Run("other movie collision is inert", func(t *testing.T) {
		db := setupBaseRepoTestDB(t)
		seedArtifactPublicationMovie(t, db, "unrelated-target", 1, true)
		seedLiveCollisionFenceFixture(t, db, "unrelated-other", models.CreditFieldCreditedName)
		assertArtifactFenceResult(t, db, "unrelated-target", false)
	})
}

func TestPR260ArtifactPublicationFenceCollisionServiceSuppressionLifecycle(t *testing.T) {
	db, service, credit, collision := collisionFixture(t)
	assertArtifactFenceResult(t, db, credit.MovieContentID, true)
	require.NoError(t, service.SetCreditSuppressed(t.Context(), credit.ID, true))
	var stored models.CreditCollision
	require.NoError(t, db.First(&stored, collision.ID).Error)
	require.Equal(t, models.CollisionStatusResolved, stored.Status)
	require.Equal(t, models.CollisionResolutionBySuppression, stored.Resolution)
	assertArtifactFenceResult(t, db, credit.MovieContentID, false)
	require.NoError(t, service.SetCreditSuppressed(t.Context(), credit.ID, false))
	require.NoError(t, db.First(&stored, collision.ID).Error)
	require.Equal(t, models.CollisionStatusOpen, stored.Status)
	assertArtifactFenceResult(t, db, credit.MovieContentID, true)
}

func TestPR260ArtifactPublicationFenceLiveCollisionQueryErrorFailsClosed(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	seedArtifactPublicationMovie(t, db, "live-query-error", 1, true)
	injectDatabaseCallbackError(t, db, "query", "credit_collisions", 1)
	called := false
	err := NewMovieRepository(db).WithApplyArtifactPublicationFence(context.Background(), "live-query-error", 1, func(*models.Movie) error {
		called = true
		return nil
	})
	require.ErrorContains(t, err, "injected database error")
	require.False(t, called)
}
