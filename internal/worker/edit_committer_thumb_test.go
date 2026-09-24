package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestUpdateMovie_ActressThumbnailEditError(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()

	badRepo := mocks.NewMockActressRepositoryInterface(t)
	badRepo.EXPECT().FindByID(mock.Anything, uint(1)).Return(
		&models.Actress{ID: 1, FirstName: "Yui", LastName: "Hatano", ThumbURL: "https://old.test/thumb.jpg"}, nil)
	badRepo.EXPECT().RenameIdentityFields(mock.Anything, uint(1), "Yui-Edited", "Hatano", "", "https://new.test/thumb.jpg").Return(errors.New("boom"))

	jq := NewJobStore(nil, nil, repos.MovieRepo, "", nil, nil, WithActressRepo(badRepo))
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: "ABC-001", Actresses: []models.Actress{
			{ID: 1, FirstName: "Yui", LastName: "Hatano", ThumbURL: "https://old.test/thumb.jpg"},
		}},
	})

	ej, ok := jq.GetJobForEdit(job.ID.String())
	require.True(t, ok)

	err := ej.UpdateMovie(context.Background(), "file1.mp4",
		&models.Movie{ID: "ABC-001", Actresses: []models.Actress{
			{ID: 1, FirstName: "Yui-Edited", LastName: "Hatano", ThumbURL: "https://new.test/thumb.jpg"},
		}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "persist actress identity edit",
		"a failing thumbnail identity edit must abort UpdateMovie with a wrapped error")
}
