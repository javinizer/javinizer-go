package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// Independent review coverage: counts are scoped to current live attachments,
// even across repeated queries and reversible state transitions. The collision
// ledger itself remains historical evidence and is never rewritten by counting.
func TestReviewCreditCollisionBatchCurrentAttachmentLifecycle(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	t.Cleanup(func() { _ = db.Close() })
	ctx := t.Context()
	credit, historical := seedLiveCollisionFenceFixture(t, db, "review-old", models.CreditFieldCreditedName)
	seedArtifactPublicationMovie(t, db, "review-current", 1, true)
	seedLiveCollisionFenceFixture(t, db, "review-unrelated", models.CreditFieldIdentityLink)

	second := models.CreditCollision{
		CreditID: credit.ID, MovieContentID: "review-old",
		Field: models.CreditFieldReportedThumb, Status: models.CollisionStatusOpen,
	}
	resolved := models.CreditCollision{
		CreditID: credit.ID, MovieContentID: "review-old",
		Field: models.CreditFieldIdentityLink, Status: models.CollisionStatusResolved,
	}
	require.NoError(t, db.Create(&second).Error)
	require.NoError(t, db.Create(&resolved).Error)
	require.NoError(t, db.Model(&models.MovieCredit{}).Where("id = ?", credit.ID).Update("movie_content_id", "review-current").Error)

	repo := NewCreditCollisionRepository(db)
	check := func(ids []string, expected map[string]int64) {
		t.Helper()
		got, err := repo.CountOpenByMovieBatch(ctx, ids)
		require.NoError(t, err)
		require.Equal(t, expected, got)
	}
	check(nil, map[string]int64{})
	check([]string{}, map[string]int64{})
	check([]string{"review-old", "missing"}, map[string]int64{})
	check([]string{"review-current", "review-current", "review-old"}, map[string]int64{"review-current": 2})
	check([]string{"review-current", "review-unrelated"}, map[string]int64{"review-current": 2, "review-unrelated": 1})
	assertArtifactFenceResult(t, db, "review-old", false)
	assertArtifactFenceResult(t, db, "review-current", true)

	require.NoError(t, NewMovieCreditRepository(db).UpdateSuppressed(ctx, credit.ID, true))
	check([]string{"review-current", "review-old", "review-unrelated"}, map[string]int64{"review-unrelated": 1})
	assertArtifactFenceResult(t, db, "review-current", false)
	require.NoError(t, NewMovieCreditRepository(db).UpdateSuppressed(ctx, credit.ID, false))
	check([]string{"review-current", "review-old"}, map[string]int64{"review-current": 2})
	assertArtifactFenceResult(t, db, "review-current", true)

	var stored models.CreditCollision
	require.NoError(t, db.First(&stored, historical.ID).Error)
	require.Equal(t, "review-old", stored.MovieContentID)
	require.Equal(t, models.CollisionStatusOpen, stored.Status)
}

func TestReviewCreditCollisionBatchReadErrorsRemainErrors(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	t.Cleanup(func() { _ = db.Close() })
	seedLiveCollisionFenceFixture(t, db, "review-read-error", models.CreditFieldCreditedName)
	repo := NewCreditCollisionRepository(db)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	counts, err := repo.CountOpenByMovieBatch(ctx, []string{"review-read-error"})
	require.Error(t, err)
	require.Nil(t, counts)

	// A failed live-credit join must propagate rather than becoming zero counts.
	require.NoError(t, db.Migrator().DropTable(&models.MovieCredit{}))
	counts, err = repo.CountOpenByMovieBatch(t.Context(), []string{"review-read-error"})
	require.Error(t, err)
	require.Nil(t, counts)
}
