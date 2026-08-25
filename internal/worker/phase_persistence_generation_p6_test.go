package worker

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	wfmocks "github.com/javinizer/javinizer-go/internal/mocks/workflow"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestStartScrape_StaleMarkerPersistFailsClosed(t *testing.T) {
	job := newBatchJob(nil)
	job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
	job.deps.PersistFn = func() error { return database.ErrStaleEnvelopeGeneration }

	err := job.Controller().StartScrape(context.Background(), nil, ScrapePhaseConfig{})
	require.ErrorIs(t, err, database.ErrStaleEnvelopeGeneration)
	require.Equal(t, models.JobStatusFailed, job.lifecycle.GetJobStatus())
}

func TestStartScrape_PhaseEndStalePersistIsBestEffort(t *testing.T) {
	job := newBatchJob(nil)
	job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
	var calls atomic.Int32
	job.deps.PersistFn = func() error {
		if calls.Add(1) == 1 {
			return nil
		}
		return database.ErrStaleEnvelopeGeneration
	}

	require.NoError(t, job.Controller().StartScrape(context.Background(), nil, ScrapePhaseConfig{}))
	require.NoError(t, job.Controller().Wait())
	require.GreaterOrEqual(t, calls.Load(), int32(2), "phase end must attempt its envelope persist")
}

func TestApplyPhase_StalePersistIsBestEffort(t *testing.T) {
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "P6-APPLY"}}}
	inputs := makeApplyInputs(wf)
	inputs.Results["/source/P6-APPLY.mp4"] = &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/source/P6-APPLY.mp4", MovieID: "P6-APPLY"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "P6-APPLY"},
	}
	var calls atomic.Int32
	inputs.persister = persistFunc(func() error {
		calls.Add(1)
		return database.ErrStaleEnvelopeGeneration
	})

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{})
	require.Equal(t, int32(1), calls.Load(), "apply must log-and-continue after its phase-end persist error")
}
