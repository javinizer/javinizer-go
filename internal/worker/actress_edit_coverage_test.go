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

// RenameNameFields must reject a zero id up front (guard for the column-
// restricted rename; covers the id==0 error branch).
func TestRenameNameFields_RejectsZeroID(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	err := repos.ActressRepo.RenameNameFields(context.Background(), 0, "A", "B", "C")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "actress id 0",
		"RenameNameFields must reject id==0 with a wrapped invalid-lookup error")
}

// A non-NotFound FindByID error in the rename loop must abort UpdateMovie with
// a wrapped "load actress for rename" error (covers that error branch).
func TestUpdateMovie_ActressRename_LoadError(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()

	badRepo := mocks.NewMockActressRepositoryInterface(t)
	badRepo.EXPECT().FindByID(mock.Anything, uint(1)).Return(nil, errors.New("db unavailable"))

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
	assert.Contains(t, err.Error(), "load actress for rename",
		"a non-NotFound FindByID error must abort UpdateMovie with a wrapped error")
}

// RenameNameFields must surface a DB error from the underlying update (covers
// the Updates error branch).
func TestRenameNameFields_DBError(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	a := seedNamedActress(t, repos.ActressRepo, "Umi", "", "")
	require.NoError(t, db.Close())
	err := repos.ActressRepo.RenameNameFields(context.Background(), a.ID, "X", "", "")
	require.Error(t, err, "a write against a closed DB must return an error")
}
