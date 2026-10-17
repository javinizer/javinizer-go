package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This regression test deliberately asks the early apply gate to agree with
// the existing final publication fence. It fails on unmodified v1.6.2.
func TestRegressionCountOpenByMovieBatchMatchesLiveAttachment(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	t.Cleanup(func() { _ = db.Close() })
	ctx := t.Context()

	seedArtifactPublicationMovie(t, db, "detached", 1, true)
	require.NoError(t, db.Create(&models.CreditCollision{
		CreditID: 999999, MovieContentID: "detached",
		Field: models.CreditFieldIdentityLink, Status: models.CollisionStatusOpen,
	}).Error)

	suppressed, _ := seedLiveCollisionFenceFixture(t, db, "suppressed", models.CreditFieldCreditedName)
	require.NoError(t, NewMovieCreditRepository(db).UpdateSuppressed(ctx, suppressed.ID, true))

	moved, _ := seedLiveCollisionFenceFixture(t, db, "moved-old", models.CreditFieldCreditedName)
	seedArtifactPublicationMovie(t, db, "moved-current", 1, true)
	require.NoError(t, db.Model(&models.MovieCredit{}).Where("id = ?", moved.ID).Update("movie_content_id", "moved-current").Error)

	seedLiveCollisionFenceFixture(t, db, "active", models.CreditFieldCreditedName)
	_, resolved := seedLiveCollisionFenceFixture(t, db, "resolved", models.CreditFieldCreditedName)
	require.NoError(t, db.Model(&models.CreditCollision{}).Where("id = ?", resolved.ID).Update("status", models.CollisionStatusResolved).Error)
	seedArtifactPublicationMovie(t, db, "clear", 1, true)

	wants := []struct {
		movieID string
		count   int64
	}{
		{"detached", 0},
		{"suppressed", 0},
		{"moved-old", 0},
		{"moved-current", 1},
		{"active", 1},
		{"resolved", 0},
		{"clear", 0},
	}
	ids := make([]string, 0, len(wants))
	for _, want := range wants {
		ids = append(ids, want.movieID)
	}
	counts, err := NewCreditCollisionRepository(db).CountOpenByMovieBatch(ctx, ids)
	require.NoError(t, err)
	for _, want := range wants {
		t.Run(want.movieID, func(t *testing.T) {
			assertArtifactFenceResult(t, db, want.movieID, want.count > 0)
			t.Logf("movie=%s early_count=%d final_fence_blocked=%t", want.movieID, counts[want.movieID], want.count > 0)
			assert.Equal(t, want.count, counts[want.movieID], "early gate must use the same live collision attachment as final publication")
		})
	}
}
