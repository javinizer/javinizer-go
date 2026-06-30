package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestSetDepsFromConfig_ActressRepo(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()

	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"}, &JobConfig{BatchJobDeps: BatchJobDeps{ActressRepo: repos.ActressRepo}})

	assert.Same(t, repos.ActressRepo, job.deps.ActressRepo,
		"setDepsFromConfig must wire cfg.ActressRepo into job.deps")
}

func TestUpdateMovie_ActressRenameError(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()

	// Mock actress repo: FindByID returns the existing (old-name) record so the
	// rename is attempted, then RenameNameFields fails. A real (open) movieRepo
	// satisfies the gate; Upsert never runs because the rename aborts first.
	badRepo := mocks.NewMockActressRepositoryInterface(t)
	badRepo.EXPECT().FindByID(mock.Anything, uint(1)).Return(
		&models.Actress{ID: 1, FirstName: "Yui", LastName: "", JapaneseName: "波多野結衣"}, nil)
	badRepo.EXPECT().RenameNameFields(mock.Anything, uint(1), "Yui-Edited", "", "波多野結衣").Return(errors.New("boom"))

	jq := NewJobStore(nil, nil, repos.MovieRepo, "", nil, nil, WithActressRepo(badRepo))
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: "ABC-001", Actresses: []models.Actress{
			{ID: 1, FirstName: "Yui", JapaneseName: "波多野結衣"},
		}},
	})

	ej, ok := jq.GetJobForEdit(job.ID.String())
	require.True(t, ok)

	err := ej.UpdateMovie(context.Background(), "file1.mp4",
		&models.Movie{ID: "ABC-001", Actresses: []models.Actress{
			{ID: 1, FirstName: "Yui-Edited", JapaneseName: "波多野結衣"},
		}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "persist actress name edit",
		"a failing actress rename must abort UpdateMovie with a wrapped error")
}

func TestReconstructBatchJob_ActressRepo(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()

	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob), actressRepo: repos.ActressRepo}
	dbJob := &models.Job{ID: "recon-actress-1", Status: models.JobStatusCompleted, TotalFiles: 1}
	reconstructed := jq.reconstructBatchJob(dbJob)

	assert.Same(t, repos.ActressRepo, reconstructed.deps.ActressRepo,
		"reconstructed jobs must inherit JobStore.actressRepo via wireJobDeps")
}
