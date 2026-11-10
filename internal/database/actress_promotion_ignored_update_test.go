package database

import (
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestPromoteCandidateIgnoredUpdatePreservesStateAndRetries(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	candidate := models.Actress{FirstName: "Old", LastName: "Candidate", JapaneseName: "旧名", Origin: ActressOriginScrape}
	require.NoError(t, db.Create(&candidate).Error)
	movie := models.Movie{ContentID: "promotion-ignore", ID: "PROMOTION-IGNORE", RenderGeneration: 12, Actresses: []models.Actress{candidate}}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID, CreditedName: "Old Candidate", OverrideName: "Local credit", UserOverride: true, Origin: string(models.CreditOriginUser)}
	require.NoError(t, db.Create(&credit).Error)
	require.NoError(t, db.Create(&models.ActressTranslation{ActressID: candidate.ID, Language: "en", DisplayName: "Old translation"}).Error)
	before := previewDatabaseState(t, db)
	// SQLite's supported IGNORE policy reports a successful statement with no
	// changed rows. Promotion must verify the write happened before invalidating
	// translations, transitioning aliases, or marking movie renders dirty.
	require.NoError(t, db.Exec(fmt.Sprintf(`CREATE TRIGGER ignore_candidate_promotion
  BEFORE UPDATE OF verified ON actresses WHEN OLD.id = %d AND NEW.verified = 1
  BEGIN SELECT RAISE(IGNORE); END`, candidate.ID)).Error)
	t.Cleanup(func() { _ = db.Exec("DROP TRIGGER IF EXISTS ignore_candidate_promotion").Error })
	promote := func() error {
		return repo.PromoteCandidate(t.Context(), candidate.ID, "New", "Candidate", "新名", "new-thumb")
	}
	require.ErrorIs(t, promote(), ErrCandidateAlreadyVerified)
	require.Equal(t, before, previewDatabaseState(t, db))
	require.NoError(t, db.Exec("DROP TRIGGER ignore_candidate_promotion").Error)
	require.NoError(t, promote())
	stored, err := repo.FindByID(t.Context(), candidate.ID)
	require.NoError(t, err)
	require.True(t, stored.Verified)
	require.Equal(t, ActressOriginUser, stored.Origin)
	require.Equal(t, "New", stored.FirstName)
	require.Equal(t, "新名", stored.JapaneseName)
	require.Equal(t, "new-thumb", stored.ThumbURL)
	var translations int64
	require.NoError(t, db.Model(&models.ActressTranslation{}).Where("actress_id = ?", candidate.ID).Count(&translations).Error)
	require.Zero(t, translations)
	var storedMovie models.Movie
	require.NoError(t, db.First(&storedMovie, "content_id = ?", movie.ContentID).Error)
	require.True(t, storedMovie.RenderDirty)
	require.Equal(t, movie.RenderGeneration+1, storedMovie.RenderGeneration)
	var storedCredit models.MovieCredit
	require.NoError(t, db.First(&storedCredit, credit.ID).Error)
	require.Equal(t, candidate.ID, storedCredit.ActressID)
	require.Equal(t, credit.OverrideName, storedCredit.OverrideName)
	require.True(t, storedCredit.UserOverride)
}
