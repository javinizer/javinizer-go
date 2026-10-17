package worker

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
	"testing"
)

type castAmbiguityBatchWorkflow struct {
	stubApplyWorkflow
	paths []string
}

func (w *castAmbiguityBatchWorkflow) Apply(_ context.Context, cmd workflow.ApplyCmd) (*workflow.ApplyResult, error) {
	w.paths = append(w.paths, cmd.Match.Path)
	result := &workflow.ApplyResult{Movie: cmd.Movie}
	if cmd.Movie.ID == "BLOCKED" {
		result.PrePublication = true
		result.FailedStep = "merge"
		return result, &nfo.CastAmbiguityError{Actors: []string{"Prior Alias"}, Reason: "unlinked actor may be a previous movie-only name"}
	}
	return result, nil
}

func TestNFOCastAmbiguityIsPerFileAndDoesNotAbortOtherApplyItems(t *testing.T) {
	wf := &castAmbiguityBatchWorkflow{}
	inputs := makeApplyInputs(wf)
	for path, id := range map[string]string{"/source/1.mp4": "BLOCKED", "/source/2.mp4": "GOOD"} {
		inputs.Results[path] = &resultstore.MovieResult{FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: id}, Status: models.JobStatusCompleted, Movie: &models.Movie{ID: id, Title: "Synthetic", Credits: []models.MovieCredit{{CreditedName: "Selected", UserOverride: true, OverrideName: "Current"}}}}
	}
	for path, result := range inputs.Results {
		inputs.Updater.UpdateFileResult(path, result.Clone())
	}
	before := inputs.Results["/source/1.mp4"].Movie.Clone()
	var organized, failed int
	NewApplyPhase().Run(t.Context(), inputs, ApplyPhaseConfig{Destination: "/output", OnPhaseComplete: func(ok, bad int) { organized, failed = ok, bad }})
	require.Equal(t, 1, organized)
	require.Equal(t, 1, failed)
	require.Len(t, wf.paths, 2)
	blocked := inputs.Updater.(*stubUpdater).getResult("/source/1.mp4")
	require.Equal(t, models.JobStatusFailed, blocked.Status)
	require.Contains(t, blocked.Error, "NFO cast requires review")
	require.Contains(t, blocked.Error, "Prior Alias")
	require.Equal(t, before.Credits, blocked.Movie.Credits)
	good := inputs.Updater.(*stubUpdater).getResult("/source/2.mp4")
	require.NotNil(t, good)
	require.Equal(t, models.JobStatusCompleted, good.Status)
	require.Empty(t, good.Error)
	require.True(t, inputs.Lifecycle.(*stubLifecycle).completed)
}
