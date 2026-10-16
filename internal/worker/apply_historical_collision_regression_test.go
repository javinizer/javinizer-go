package worker

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The real repository and apply phase run against an isolated local SQLite
// database. Only the workflow's actual file-operation endpoint is stubbed.
// These synthetic cases are not evidence about any reporter's database.
func TestRegressionApplyPhaseIgnoresHistoricalCollisionEvidence(t *testing.T) {
	for _, kind := range []string{"detached", "suppressed", "moved-old", "active", "resolved"} {
		t.Run(kind, func(t *testing.T) {
			db := newActressEditTestDB(t)
			ctx := t.Context()
			movie := models.Movie{ID: "target", ContentID: "target", RenderGeneration: 1, RenderDirty: true}
			require.NoError(t, db.Create(&movie).Error)
			actress := models.Actress{FirstName: "Synthetic", Verified: true, Origin: "user"}
			require.NoError(t, db.Create(&actress).Error)
			credit := models.MovieCredit{ID: 999999, MovieContentID: movie.ContentID, ActressID: actress.ID}
			if kind != "detached" {
				credit.ID = 0
				require.NoError(t, db.Create(&credit).Error)
			}
			collision := models.CreditCollision{
				CreditID: credit.ID, MovieContentID: movie.ContentID,
				Field: models.CreditFieldCreditedName, Status: models.CollisionStatusOpen,
				ReportedValue: "Reported", CanonicalValue: "Synthetic",
			}
			require.NoError(t, db.Create(&collision).Error)
			switch kind {
			case "suppressed":
				require.NoError(t, database.NewMovieCreditRepository(db).UpdateSuppressed(ctx, credit.ID, true))
			case "moved-old":
				require.NoError(t, db.Create(&models.Movie{ID: "other", ContentID: "other"}).Error)
				require.NoError(t, db.Model(&models.MovieCredit{}).Where("id = ?", credit.ID).Update("movie_content_id", "other").Error)
			case "resolved":
				require.NoError(t, db.Model(&models.CreditCollision{}).Where("id = ?", collision.ID).Update("status", models.CollisionStatusResolved).Error)
			}

			// Confirm the production final fence's decision before running the
			// early apply gate on exactly the same persisted state.
			require.NoError(t, db.First(&movie, "content_id = ?", movie.ContentID).Error)
			fenceCalled := false
			fenceErr := database.NewMovieRepository(db).WithApplyArtifactPublicationFence(ctx, movie.ContentID, movie.RenderGeneration, func(*models.Movie) error {
				fenceCalled = true
				return nil
			})
			wantBlocked := kind == "active"
			if wantBlocked {
				require.ErrorIs(t, fenceErr, database.ErrApplyArtifactPublicationBlocked)
				require.False(t, fenceCalled)
			} else {
				require.NoError(t, fenceErr)
				require.True(t, fenceCalled)
			}

			wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &movie}}
			inputs := makeApplyInputs(wf)
			inputs.CollisionRepo = database.NewCreditCollisionRepository(db)
			result := &resultstore.MovieResult{Status: models.JobStatusCompleted, Movie: &movie}
			inputs.Results = map[string]*resultstore.MovieResult{"synthetic.mp4": result}
			inputs.Updater.UpdateFileResult("synthetic.mp4", result)
			var organized, failed int
			NewApplyPhase().Run(ctx, inputs, ApplyPhaseConfig{
				Destination:     t.TempDir(),
				OnPhaseComplete: func(org, fail int) { organized, failed = org, fail },
			})
			stored := inputs.Updater.(*stubUpdater).getResult("synthetic.mp4")
			require.NotNil(t, stored)
			t.Logf("fixture=%s final_fence_blocked=%t apply_calls=%d organized=%d failed=%d status=%s code=%q error=%q", kind, wantBlocked, wf.getApplyCalled(), organized, failed, stored.Status, stored.ErrorCode, stored.Error)
			if wantBlocked {
				assert.Equal(t, 0, wf.getApplyCalled())
				assert.Equal(t, 1, failed)
				assert.Equal(t, models.JobStatusFailed, stored.Status)
				assert.Contains(t, stored.Error, "open credit collision")
			} else {
				assert.Equal(t, 1, wf.getApplyCalled(), "safe historical evidence must not prevent workflow apply")
				assert.Equal(t, 1, organized)
				assert.Equal(t, 0, failed)
			}
		})
	}
}
