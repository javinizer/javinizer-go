package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestActressThumbIntentRequiresBaselineChange(t *testing.T) {
	baseline := &models.Movie{Actresses: []models.Actress{{ID: 1, ThumbURL: "https://old.test/thumb.jpg"}}}

	edited, _ := actressThumbIntent(nil, models.Actress{ID: 1, ThumbURL: "https://new.test/thumb.jpg"})
	require.False(t, edited, "no baseline means no proven intent")
	edited, _ = actressThumbIntent(baseline, models.Actress{ID: 2, ThumbURL: "https://new.test/thumb.jpg"})
	require.False(t, edited, "unknown actress means no proven intent")
	edited, _ = actressThumbIntent(baseline, models.Actress{ID: 1, ThumbURL: "https://old.test/thumb.jpg"})
	require.False(t, edited, "unchanged thumbnail is not an edit")
	edited, base := actressThumbIntent(baseline, models.Actress{ID: 1, ThumbURL: "https://new.test/thumb.jpg"})
	require.True(t, edited, "a changed thumbnail is an edit")
	require.Equal(t, "https://old.test/thumb.jpg", base, "the baseline travels with the intent for the drift guard")
	edited, _ = actressThumbIntent(baseline, models.Actress{ID: 1, ThumbURL: ""})
	require.False(t, edited, "an omitted or empty thumbnail must not clear a shared identity")
	edited, _ = actressThumbIntent(baseline, models.Actress{ID: 1, ThumbURL: "   "})
	require.False(t, edited, "whitespace-only thumbnails are treated as absent")
}

func TestUpdateMovieUnrelatedSaveKeepsDatabaseThumbnail(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()

	identity := models.Actress{FirstName: "Yui", LastName: "Hatano", ThumbURL: "https://newer.test/thumb.jpg", Verified: true, Origin: "user"}
	require.NoError(t, db.Create(&identity).Error)
	movie := models.Movie{ContentID: "THUMB-REG", ID: "THUMB-REG", Title: "Title"}
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: identity.ID, CreditedName: "Hatano Yui", OrderPinned: true}).Error)
	require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{identity}))

	jq := NewJobStore(nil, nil, repos.MovieRepo, "", nil, nil, WithActressRepo(repos.ActressRepo))
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	staleThumb := "https://old.test/thumb.jpg"
	job.results.UpdateFileResult("file1.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: movie.ID},
		Status:        models.JobStatusCompleted,
		// The cached job baseline still carries the pre-update thumbnail.
		Movie: &models.Movie{ID: movie.ID, Title: "Title", Actresses: []models.Actress{
			{ID: identity.ID, FirstName: "Yui", LastName: "Hatano", ThumbURL: staleThumb},
		}},
	})
	ej, ok := jq.GetJobForEdit(job.ID.String())
	require.True(t, ok)

	// Unrelated title edit: the actress payload repeats the stale thumbnail.
	require.NoError(t, ej.UpdateMovie(context.Background(), "file1.mp4", &models.Movie{ID: movie.ID, Title: "Title v2", Actresses: []models.Actress{
		{ID: identity.ID, FirstName: "Yui", LastName: "Hatano", ThumbURL: staleThumb},
	}}))

	var stored models.Actress
	require.NoError(t, db.First(&stored, identity.ID).Error)
	require.Equal(t, "https://newer.test/thumb.jpg", stored.ThumbURL, "an unrelated save must not revert a thumbnail changed elsewhere")

	// A name-only payload omits thumb_url, which decodes to an empty string; it
	// must not be mistaken for a deliberate clear of the shared identity.
	require.NoError(t, ej.UpdateMovie(context.Background(), "file1.mp4", &models.Movie{ID: movie.ID, Title: "Title v3", Actresses: []models.Actress{
		{ID: identity.ID, FirstName: "Yui", LastName: "Hatano", ThumbURL: ""},
	}}))

	require.NoError(t, db.First(&stored, identity.ID).Error)
	require.Equal(t, "https://newer.test/thumb.jpg", stored.ThumbURL, "a sparse payload must not clear the shared thumbnail")
}
