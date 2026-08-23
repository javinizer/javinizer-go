package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

type parallelPersistRepo struct {
	mockJobRepoForPersist
	armed   atomic.Bool
	entered chan struct{}
	release <-chan struct{}
}

func (r *parallelPersistRepo) Upsert(_ context.Context, _ *models.Job) error {
	if r.armed.Load() && r.armed.CompareAndSwap(true, false) {
		close(r.entered)
		<-r.release
	}
	return nil
}

// TestJobStorePersist_MutationWorkStaysParallel stalls one envelope upsert.
// The other movie's in-memory mutation must publish before the first upsert is
// released; only the shared marshal+upsert section is serialized.
func TestJobStorePersist_MutationWorkStaysParallel(t *testing.T) {
	release := make(chan struct{})
	repo := &parallelPersistRepo{entered: make(chan struct{}), release: release}
	store := NewJobStore(repo, nil, nil, "", nil, nil)
	job := store.CreateJobBatch([]string{"/v/a.mp4", "/v/b.mp4"})
	job.results.UpdateFileResult("/v/a.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/v/a.mp4", MovieID: "A-1"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "A-1", Title: "a"},
	})
	job.results.UpdateFileResult("/v/b.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/v/b.mp4", MovieID: "B-1"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "B-1", Title: "b"},
	})
	job.Controller().SetJobStatus(models.JobStatusCompleted)
	repo.armed.Store(true)

	errs := make(chan error, 2)
	go func() {
		errs <- job.posterEditor.UpdateMovieFamily(context.Background(), "A-1", "", &models.Movie{ID: "A-1", Title: "a-edit"}, FamilySaveOptions{})
	}()
	select {
	case <-repo.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first persist did not reach the stalled upsert")
	}

	go func() {
		errs <- job.posterEditor.UpdateMovieFamily(context.Background(), "B-1", "", &models.Movie{ID: "B-1", Title: "b-edit"}, FamilySaveOptions{})
	}()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		result, err := job.results.GetMovieResult("/v/b.mp4")
		if err == nil && result != nil && result.Movie != nil && result.Movie.Title == "b-edit" {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("second movie mutation stayed blocked behind the first envelope upsert")
		case <-time.After(10 * time.Millisecond):
		}
	}
	close(release)
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
}
