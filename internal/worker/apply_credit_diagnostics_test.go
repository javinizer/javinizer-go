package worker

import (
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestApplyCreditGateFailureDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		counts map[string]int64
		err    error
		code   string
		reason string
	}{
		{"conflict", map[string]int64{"movie": 1}, nil, models.FileErrorCodeCreditConflict, "apply blocked by an open credit collision"},
		{"lookup failure", nil, errors.New("database unavailable"), models.FileErrorCodeCreditStatusUnavailable, "apply blocked because credit collision status is unavailable"},
	} {
		for _, preexisting := range []bool{false, true} {
			name := tc.name + "/fallback"
			if preexisting {
				name = tc.name + "/atomic update"
			}
			t.Run(name, func(t *testing.T) {
				repo := mocks.NewMockCreditCollisionRepositoryInterface(t)
				repo.EXPECT().CountOpenByMovieBatch(mock.Anything, []string{"movie"}).Return(tc.counts, tc.err).Once()
				wf := &stubApplyWorkflow{}
				inputs := makeApplyInputs(wf)
				inputs.CollisionRepo = repo
				original := &resultstore.MovieResult{
					Status: models.JobStatusCompleted,
					Movie:  &models.Movie{ID: "movie", ContentID: "movie"},
				}
				inputs.Results["synthetic.mp4"] = original
				if preexisting {
					inputs.Updater.UpdateFileResult("synthetic.mp4", original.Clone())
				}
				var organized, failed int
				var failedPath, failedReason string
				NewApplyPhase().Run(t.Context(), inputs, ApplyPhaseConfig{
					Destination:     t.TempDir(),
					OnPhaseComplete: func(org, fail int) { organized, failed = org, fail },
					OnFileFailed:    func(path, reason string) { failedPath, failedReason = path, reason },
				})
				stored := inputs.Updater.(*stubUpdater).getResult("synthetic.mp4")
				require.NotNil(t, stored)
				assert.Equal(t, tc.code, stored.ErrorCode)
				assert.NotEqual(t, string(models.ScraperErrorKindBlocked), stored.ErrorCode)
				assert.Equal(t, tc.reason, stored.Error)
				assert.Equal(t, models.JobStatusFailed, stored.Status)
				assert.NotNil(t, stored.EndedAt)
				assert.Zero(t, wf.getApplyCalled())
				assert.Zero(t, organized)
				assert.Equal(t, 1, failed)
				assert.Equal(t, "synthetic.mp4", failedPath)
				assert.Equal(t, tc.reason, failedReason)
				events := inputs.Broadcaster.(*stubBroadcaster).events
				require.Len(t, events, 1)
				assert.Equal(t, StepFailed, events[0].Step)
				assert.Equal(t, tc.reason, events[0].Message)
			})
		}
	}
}

func TestApplyRetryReplacesCreditGateDiagnostics(t *testing.T) {
	for _, oldCode := range []string{models.FileErrorCodeCreditConflict, models.FileErrorCodeCreditStatusUnavailable} {
		for _, outcome := range []struct {
			name   string
			err    error
			status models.JobStatus
			code   string
		}{
			{"success", nil, models.JobStatusCompleted, ""},
			{"unrelated failure", errors.New("disk full"), models.JobStatusFailed, ""},
			{"poster failure", downloader.ErrPosterRecropRequired, models.JobStatusFailed, downloader.PosterRecropRequiredCode},
		} {
			t.Run(oldCode+"/"+outcome.name, func(t *testing.T) {
				repo := mocks.NewMockCreditCollisionRepositoryInterface(t)
				repo.EXPECT().CountOpenByMovieBatch(mock.Anything, []string{"movie"}).Return(map[string]int64{}, nil).Once()
				movie := &models.Movie{ID: "movie", ContentID: "movie"}
				wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: movie}, applyErr: outcome.err}
				inputs := makeApplyInputs(wf)
				inputs.CollisionRepo = repo
				original := &resultstore.MovieResult{Status: models.JobStatusFailed, Movie: movie, Error: "old gate failure", ErrorCode: oldCode}
				inputs.Results["synthetic.mp4"] = original
				inputs.Updater.UpdateFileResult("synthetic.mp4", original.Clone())
				NewApplyPhase().Run(t.Context(), inputs, ApplyPhaseConfig{
					Destination: t.TempDir(), RetryFilePaths: []string{"synthetic.mp4"},
				})
				stored := inputs.Updater.(*stubUpdater).getResult("synthetic.mp4")
				require.NotNil(t, stored)
				assert.Equal(t, 1, wf.getApplyCalled())
				assert.Equal(t, outcome.status, stored.Status)
				assert.Equal(t, outcome.code, stored.ErrorCode)
				if outcome.err == nil {
					assert.Empty(t, stored.Error)
				} else {
					assert.Equal(t, outcome.err.Error(), stored.Error)
				}
			})
		}
	}
}

func TestClearPriorCreditGateErrorPreservesOtherDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name     string
		current  string
		baseline *resultstore.MovieResult
		want     string
	}{
		{"no snapshot", models.FileErrorCodeCreditConflict, nil, models.FileErrorCodeCreditConflict},
		{"newer diagnostic", models.FileErrorCodeCreditStatusUnavailable, &resultstore.MovieResult{ErrorCode: models.FileErrorCodeCreditConflict}, models.FileErrorCodeCreditStatusUnavailable},
		{"scraper blocked", string(models.ScraperErrorKindBlocked), &resultstore.MovieResult{ErrorCode: string(models.ScraperErrorKindBlocked)}, string(models.ScraperErrorKindBlocked)},
		{"poster recrop", downloader.PosterRecropRequiredCode, &resultstore.MovieResult{ErrorCode: downloader.PosterRecropRequiredCode}, downloader.PosterRecropRequiredCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := &resultstore.MovieResult{ErrorCode: tc.current}
			clearPriorCreditGateError(current, tc.baseline)
			assert.Equal(t, tc.want, current.ErrorCode)
		})
	}
}
