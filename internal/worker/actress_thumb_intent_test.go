package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestActressThumbEditedRequiresExplicitIntent(t *testing.T) {
	require.False(t, actressThumbEdited(models.Actress{ID: 1, ThumbURL: "https://new.test/thumb.jpg"}), "an unmarked thumbnail is not an edit")
	require.False(t, actressThumbEdited(models.Actress{ID: 1, ThumbURL: "", ThumbEdited: false}), "an omitted field is not an edit")
	require.True(t, actressThumbEdited(models.Actress{ID: 1, ThumbURL: "", ThumbEdited: true}), "an explicit clear is an edit")
	require.True(t, actressThumbEdited(models.Actress{ID: 1, ThumbURL: "   ", ThumbEdited: true}), "explicit intent governs even a whitespace value")
	require.True(t, actressThumbEdited(models.Actress{ID: 1, ThumbURL: "https://new.test/thumb.jpg", ThumbEdited: true}), "only an explicit client edit counts")
}

// An explicit clear (thumb_edited=true with an empty URL) must reach the
// identity rename leg: the movie upsert merge is fill-only, so without the
// RenameIdentityFields call the stored thumbnail survives the save and the
// next refresh restores it — the editor could never clear a thumbnail.
func TestUpdateMovieExplicitThumbnailClearIsHonored(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()

	identity := models.Actress{FirstName: "Yui", LastName: "Hatano", ThumbURL: "https://old.test/thumb.jpg", Verified: true, Origin: "user"}
	require.NoError(t, db.Create(&identity).Error)
	movie := models.Movie{ContentID: "THUMB-CLEAR", ID: "THUMB-CLEAR", Title: "Title"}
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
		{ID: identity.ID, FirstName: "Yui", LastName: "Hatano", ThumbURL: "", ThumbEdited: true},
	}}))

	var stored models.Actress
	require.NoError(t, db.First(&stored, identity.ID).Error)
	require.Empty(t, stored.ThumbURL, "an explicit clear must empty the shared identity thumbnail")
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
