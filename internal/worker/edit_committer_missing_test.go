package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestUpdateMovie_ActressMissingSkipsRename(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()

	missing := mocks.NewMockActressRepositoryInterface(t)
	// FindByID returns no record and no error: the legacy rename leg must skip
	// the row instead of aborting the save.
	missing.EXPECT().FindByID(mock.Anything, uint(9)).Return(nil, nil)

	jq := NewJobStore(nil, nil, repos.MovieRepo, "", nil, nil, WithActressRepo(missing))
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-002"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: "ABC-002", Actresses: []models.Actress{
			{ID: 9, FirstName: "Ghost", LastName: "Actress"},
		}},
	})

	ej, ok := jq.GetJobForEdit(job.ID.String())
	require.True(t, ok)

	require.NoError(t, ej.UpdateMovie(context.Background(), "file1.mp4",
		&models.Movie{ID: "ABC-002", Actresses: []models.Actress{
			{ID: 9, FirstName: "Ghost-Edited", LastName: "Actress"},
		}}))
}
