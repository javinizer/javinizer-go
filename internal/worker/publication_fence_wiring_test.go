package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

func TestApplyPhasePassesRepositoryPublicationFenceToWorkflowPR260(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	fencer, ok := repos.MovieRepo.(database.ApplyPublicationFencer)
	require.True(t, ok)

	movie := &models.Movie{ID: "PR260-WIRING", ContentID: "pr260-wiring", Title: "Wiring"}
	require.NoError(t, db.Create(movie).Error)
	path := "/source/PR260-WIRING.mp4"
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: movie}}
	inputs := makeApplyInputs(wf)
	inputs.MovieRepo = repos.MovieRepo
	inputs.Results[path] = &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: movie.ID},
		Status:        models.JobStatusCompleted,
		Movie:         movie,
	}

	NewApplyPhase().Run(context.Background(), inputs, ApplyPhaseConfig{
		Destination:     "/output",
		OrganizeOptions: workflow.OrganizeOptions{MoveFiles: true},
	})

	require.Equal(t, 1, wf.getApplyCalled())
	require.Same(t, fencer, wf.getLastCmd().PublicationFence)
}

func TestBatchJobFactoryCarriesPublicationFenceToJobConfigPR260(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	fencer, ok := repos.MovieRepo.(database.ApplyPublicationFencer)
	require.True(t, ok)

	factory := NewBatchJobFactory(nil, nil, nil, nil, BatchJobConfig{}, nil, fencer)
	cfg := factory.(*batchJobFactory).buildJobConfig(BatchJobOptions{})
	require.Same(t, fencer, cfg.PublicationFence)
}
