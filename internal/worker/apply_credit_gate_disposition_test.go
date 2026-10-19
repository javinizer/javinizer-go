package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestApplyAllBlockedDispositionRequiresConfirmedCollisions(t *testing.T) {
	for _, tc := range []struct {
		name              string
		lookupErr         error
		cancel            bool
		code, reason      string
		completed, failed bool
	}{
		{name: "confirmed-collisions", code: models.FileErrorCodeCreditConflict, reason: "apply blocked by an open credit collision", completed: true},
		{name: "lookup-unavailable", lookupErr: errors.New("database unavailable"), code: models.FileErrorCodeCreditStatusUnavailable, reason: "apply blocked because credit collision status is unavailable", failed: true},
		{name: "cancelled-lookup", lookupErr: context.Canceled, cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			repo := mocks.NewMockCreditCollisionRepositoryInterface(t)
			counts := map[string]int64{"one": 1, "two": 1}
			if tc.lookupErr != nil {
				counts = nil
			}
			repo.EXPECT().CountOpenByMovieBatch(mock.Anything, mock.Anything).Return(counts, tc.lookupErr).Once()
			wf := &stubApplyWorkflow{}
			inputs := makeApplyInputs(wf)
			inputs.CollisionRepo = repo
			inputs.Results = map[string]*resultstore.MovieResult{}
			for _, id := range []string{"one", "two"} {
				path := id + ".mkv"
				row := &resultstore.MovieResult{FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: id}, Movie: &models.Movie{ID: id, ContentID: id}, Status: models.JobStatusCompleted}
				inputs.Results[path] = row
				inputs.Updater.UpdateFileResult(path, row.Clone())
			}
			phaseCalls, organized, failed := 0, 0, 0
			failedPaths := map[string]bool{}
			NewApplyPhase().Run(ctx, inputs, ApplyPhaseConfig{Destination: t.TempDir(), OnPhaseComplete: func(o, f int) { phaseCalls++; organized = o; failed = f }, OnFileFailed: func(path, reason string) { require.Equal(t, tc.reason, reason); failedPaths[path] = true }})
			lifecycle := inputs.Lifecycle.(*stubLifecycle)
			require.Equal(t, tc.completed, lifecycle.completed)
			require.Equal(t, tc.failed, lifecycle.failed)
			require.Equal(t, tc.cancel, lifecycle.cancelled)
			require.False(t, lifecycle.organized)
			require.Zero(t, wf.getApplyCalled(), "neither confirmed blocks nor an unavailable/cancelled lookup may invoke the workflow")
			wantFailures := 2
			if tc.cancel {
				wantFailures = 0
				require.Zero(t, phaseCalls)
			} else {
				require.Equal(t, 1, phaseCalls)
			}
			require.Zero(t, organized)
			require.Equal(t, wantFailures, failed)
			require.Len(t, failedPaths, wantFailures)
			require.Len(t, inputs.Broadcaster.(*stubBroadcaster).events, wantFailures)
			for path := range inputs.Results {
				row := inputs.Updater.(*stubUpdater).getResult(path)
				require.NotNil(t, row)
				if tc.cancel {
					require.Equal(t, models.JobStatusCompleted, row.Status)
					require.Empty(t, row.ErrorCode)
					require.Nil(t, row.EndedAt)
				} else {
					require.True(t, failedPaths[path])
					require.Equal(t, models.JobStatusFailed, row.Status)
					require.Equal(t, tc.code, row.ErrorCode)
					require.Equal(t, tc.reason, row.Error)
					require.NotNil(t, row.EndedAt)
				}
			}
		})
	}
}
