package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/stretchr/testify/require"
)

type stubPosterGenForDeps struct{}

func (stubPosterGenForDeps) GeneratePoster(context.Context, string, *models.Movie) error {
	return nil
}

var _ poster.PosterGenerator = stubPosterGenForDeps{}

// setDepsFromConfig: every non-nil dependency field lands in the job deps
// (BatchFileOpRepo/MovieRepo/ActressRepo/HistoryRepo/Emitter/PersistFn arms).
func TestSetDepsFromConfigAllArms(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	opRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	movieRepo := mocks.NewMockMovieRepositoryInterface(t)
	actressRepo := mocks.NewMockActressRepositoryInterface(t)
	historyRepo := mocks.NewMockHistoryRepositoryInterface(t)
	events := mocks.NewMockEventEmitter(t)
	job.controller.setDepsFromConfig(&JobConfig{BatchJobDeps: BatchJobDeps{
		Matcher:         &stubMatcher{result: ""},
		PosterGen:       stubPosterGenForDeps{},
		BatchFileOpRepo: opRepo,
		MovieRepo:       movieRepo,
		ActressRepo:     actressRepo,
		HistoryRepo:     historyRepo,
		Emitter:         events,
		PersistFn:       func() error { return nil },
		Logger:          logging.GlobalLogger(),
	}})
	require.NotNil(t, job.deps.MovieRepo)
	require.NotNil(t, job.deps.ActressRepo)
	require.NotNil(t, job.deps.HistoryRepo)
	require.NotNil(t, job.deps.Emitter)
}
