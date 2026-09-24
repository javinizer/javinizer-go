package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

// The non-committer rename leg must persist an explicit thumbnail edit too.
func TestUpdateMovieLegacyLegPersistsExplicitThumbnailEdit(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()

	identity := models.Actress{FirstName: "Yui", LastName: "Hatano", ThumbURL: "https://old.test/thumb.jpg", Verified: true, Origin: "user"}
	require.NoError(t, db.Create(&identity).Error)
	movie := models.Movie{ContentID: "THUMB-LEGACY", ID: "THUMB-LEGACY", Title: "Title"}
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: identity.ID, CreditedName: "Hatano Yui", OrderPinned: true}).Error)
	require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{identity}))

	jq := NewJobStore(nil, nil, repos.MovieRepo, "", nil, nil, WithActressRepo(repos.ActressRepo))
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: movie.ID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movie.ID, Title: "Title", Actresses: []models.Actress{
			{ID: identity.ID, FirstName: "Yui", LastName: "Hatano", ThumbURL: "https://old.test/thumb.jpg"},
		}},
	})
	ej, ok := jq.GetJobForEdit(job.ID.String())
	require.True(t, ok)

	require.NoError(t, ej.UpdateMovie(context.Background(), "file1.mp4", &models.Movie{ID: movie.ID, Title: "Title", Actresses: []models.Actress{
		{ID: identity.ID, FirstName: "Yui", LastName: "Hatano", ThumbURL: "https://new.test/thumb.jpg", ThumbEdited: true},
	}}))

	var stored models.Actress
	require.NoError(t, db.First(&stored, identity.ID).Error)
	require.Equal(t, "https://new.test/thumb.jpg", stored.ThumbURL, "the legacy rename leg must persist explicit thumbnail edits")
}
