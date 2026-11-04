package database

import (
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPromoteCandidateDismissalReadFailurePreservesState(t *testing.T) {
	for _, dismissed := range []bool{false, true} {
		name := "active candidate"
		if dismissed {
			name = "dismissed candidate"
		}
		t.Run(name, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			candidate := models.Actress{FirstName: "Old", LastName: "Candidate", JapaneseName: "旧名", Origin: ActressOriginScrape}
			require.NoError(t, db.Create(&candidate).Error)
			movie := models.Movie{ContentID: "promotion-read-contract", ID: "PROMOTION-READ", RenderGeneration: 12, Actresses: []models.Actress{candidate}}
			require.NoError(t, db.Create(&movie).Error)
			credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID, CreditedName: "Old Candidate", OverrideName: "Local credit", UserOverride: true, Origin: string(models.CreditOriginUser)}
			require.NoError(t, db.Create(&credit).Error)
			require.NoError(t, db.Create(&models.ActressTranslation{ActressID: candidate.ID, Language: "en", DisplayName: "Old translation"}).Error)
			require.NoError(t, repo.SetCandidateDismissed(t.Context(), candidate.ID, dismissed))
			before := previewDatabaseState(t, db)

			// Fail the actual dismissal lookup, not identity loading or an update.
			// A failed read must never be treated as an empty (not dismissed) result.
			failure := errors.New("dismissal lookup unavailable")
			const callback = "test:promotion-dismissal-read-failure"
			reads := 0
			require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == "actress_candidate_dismissals" {
					reads++
					_ = tx.AddError(failure)
				}
			}))
			t.Cleanup(func() { _ = db.Callback().Query().Remove(callback) })
			promote := func() error {
				return repo.PromoteCandidate(t.Context(), candidate.ID, "New", "Candidate", "新名", "new-thumb")
			}
			require.ErrorIs(t, promote(), failure)
			require.Equal(t, 1, reads, "non-lock lookup failures must not be retried")
			require.NoError(t, db.Callback().Query().Remove(callback))
			require.Equal(t, before, previewDatabaseState(t, db), "failed promotion must preserve identity, credits, translation, disposition, and render state")

			if dismissed {
				require.ErrorIs(t, promote(), ErrCandidateDismissed)
				require.Equal(t, before, previewDatabaseState(t, db), "recovering the read must still enforce dismissal")
				require.NoError(t, repo.SetCandidateDismissed(t.Context(), candidate.ID, false))
			}
			require.NoError(t, promote())
			stored, err := repo.FindByID(t.Context(), candidate.ID)
			require.NoError(t, err)
			require.True(t, stored.Verified)
			require.Equal(t, ActressOriginUser, stored.Origin)
			require.Equal(t, "New", stored.FirstName)
			require.Equal(t, "新名", stored.JapaneseName)
			require.Equal(t, "new-thumb", stored.ThumbURL)
			var storedMovie models.Movie
			require.NoError(t, db.First(&storedMovie, "content_id = ?", movie.ContentID).Error)
			require.True(t, storedMovie.RenderDirty)
			require.Equal(t, movie.RenderGeneration+1, storedMovie.RenderGeneration)
			var storedCredit models.MovieCredit
			require.NoError(t, db.First(&storedCredit, credit.ID).Error)
			require.Equal(t, candidate.ID, storedCredit.ActressID)
			require.Equal(t, credit.OverrideName, storedCredit.OverrideName)
			require.True(t, storedCredit.UserOverride)
			var translations int64
			require.NoError(t, db.Model(&models.ActressTranslation{}).Where("actress_id = ?", candidate.ID).Count(&translations).Error)
			require.Zero(t, translations, "successful canonical rename must invalidate old translations")
		})
	}
}
